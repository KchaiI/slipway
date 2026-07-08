//go:build e2e

// Package e2e verifies minato's Definition of Done against a live kind
// cluster (run via `make e2e`):
//
//  1. git push serves HTTP 200 at the issued URL within 90 seconds
//  2. a second app does not interfere with the first (namespace isolation)
//  3. deploy -> scale -> logs -> rollback all work through the CLI
package e2e

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	minatoBin = envOr("MINATO_BIN", "minato")
	urlSuffix = os.Getenv("MINATO_URL_SUFFIX") // ":8080" when ingress is not on port 80
	repoRoot  = envOr("REPO_ROOT", "../..")
)

const (
	alpha = "e2e-alpha"
	beta  = "e2e-beta"
)

func TestEndToEnd(t *testing.T) {
	for _, app := range []string{alpha, beta} {
		exec.Command(minatoBin, "apps", "destroy", app).Run()
	}
	t.Cleanup(func() {
		for _, app := range []string{alpha, beta} {
			exec.Command(minatoBin, "apps", "destroy", app).Run()
		}
	})

	alphaRepo := t.TempDir()
	// Ordered scenario: each stage builds on the previous one.
	stages := []struct {
		name string
		fn   func(*testing.T, string)
	}{
		{"PushToLiveWithin90s", testPushToLive},
		{"SecondAppIsolation", testIsolation},
		{"ScaleLogsRollback", testOpsFlow},
	}
	for _, s := range stages {
		if !t.Run(s.name, func(t *testing.T) { s.fn(t, alphaRepo) }) {
			t.Fatalf("stage %s failed; aborting dependent stages", s.name)
		}
	}
}

// --- DoD 1: push -> 200 within 90 seconds -----------------------------------

func testPushToLive(t *testing.T, repo string) {
	mustRun(t, minatoBin, "apps", "create", alpha)
	prepareRepo(t, repo, alpha, "alpha rev 1")

	start := time.Now()
	mustRun(t, "git", "-C", repo, "push", "minato", "main")
	body := waitForBody(t, appURL(alpha), "alpha rev 1", start.Add(90*time.Second))
	elapsed := time.Since(start)

	t.Logf("push -> HTTP 200 in %s (%q)", elapsed.Round(time.Second), body)
	if elapsed > 90*time.Second {
		t.Fatalf("took %s, exceeds the 90s budget", elapsed)
	}
}

// --- DoD 2: two apps do not interfere ---------------------------------------

func testIsolation(t *testing.T, _ string) {
	mustRun(t, minatoBin, "apps", "create", beta)
	betaRepo := t.TempDir()
	prepareRepo(t, betaRepo, beta, "beta rev 1")
	mustRun(t, "git", "-C", betaRepo, "push", "minato", "main")
	waitForBody(t, appURL(beta), "beta rev 1", time.Now().Add(90*time.Second))

	// Both apps serve their own content.
	if body := httpGet(t, appURL(alpha)); !strings.Contains(body, "alpha rev 1") {
		t.Fatalf("alpha no longer serves its content after beta's deploy: %q", body)
	}

	// No resource of one app exists in the other's namespace.
	for _, c := range []struct{ ns, unexpected string }{
		{"minato-app-" + alpha, beta},
		{"minato-app-" + beta, alpha},
	} {
		out := mustRun(t, "kubectl", "-n", c.ns, "get", "deploy,svc,ingress,pods", "-o", "name")
		if strings.Contains(out, c.unexpected) {
			t.Fatalf("namespace %s contains resources of %s:\n%s", c.ns, c.unexpected, out)
		}
	}

	// Destroying beta must not affect alpha.
	mustRun(t, minatoBin, "apps", "destroy", beta)
	if body := httpGet(t, appURL(alpha)); !strings.Contains(body, "alpha rev 1") {
		t.Fatalf("alpha broken by beta's destruction: %q", body)
	}
	t.Log("apps are namespace-isolated; destroying one leaves the other serving")
}

// --- DoD 3: deploy -> scale -> logs -> rollback via the CLI -----------------

func testOpsFlow(t *testing.T, repo string) {
	// Second release so there is something to roll back from.
	replaceInFile(t, filepath.Join(repo, "main.go"), "alpha rev 1", "alpha rev 2")
	mustRun(t, "git", "-C", repo,
		"-c", "user.email=e2e@minato.local", "-c", "user.name=e2e",
		"commit", "-qam", "rev 2")
	mustRun(t, "git", "-C", repo, "push", "minato", "main")
	waitForBody(t, appURL(alpha), "alpha rev 2", time.Now().Add(90*time.Second))

	// Scale to 3 and verify.
	out := mustRun(t, minatoBin, "scale", "web=3", "-a", alpha)
	if !strings.Contains(out, "3/3 ready") {
		t.Fatalf("scale did not reach 3/3: %q", out)
	}
	t.Log("scaled to web=3 (3/3 ready)")

	// Realtime logs: a fresh request must appear in `logs -f` within 5s.
	marker := fmt.Sprintf("/e2e-marker-%d", os.Getpid())
	logs := exec.Command(minatoBin, "logs", "-f", "-a", alpha)
	stdout, err := logs.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := logs.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { logs.Process.Kill(); logs.Wait() }()

	seen := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if strings.Contains(sc.Text(), marker) {
				seen <- sc.Text()
				return
			}
		}
	}()
	time.Sleep(2 * time.Second) // let the stream attach
	http.Get(appURL(alpha) + marker)
	select {
	case line := <-seen:
		if !strings.HasPrefix(line, "["+alpha+"-") {
			t.Fatalf("log line is not pod-prefixed: %q", line)
		}
		t.Logf("request streamed via logs -f: %q", line)
	case <-time.After(5 * time.Second):
		t.Fatal("request did not appear in logs -f within 5s")
	}

	// Rollback: rev 1 serves again, recorded as a new release.
	out = mustRun(t, minatoBin, "rollback", "-a", alpha)
	if !strings.Contains(out, "rollback to v1") {
		t.Fatalf("unexpected rollback output: %q", out)
	}
	waitForBody(t, appURL(alpha), "alpha rev 1", time.Now().Add(60*time.Second))
	rels := mustRun(t, minatoBin, "releases", "-a", alpha)
	if !strings.Contains(rels, "rollback to v1") {
		t.Fatalf("releases does not record the rollback:\n%s", rels)
	}
	t.Log("rollback restored rev 1 as a new release")
}

// --- helpers -----------------------------------------------------------------

func appURL(app string) string {
	return fmt.Sprintf("http://%s.localtest.me%s", app, urlSuffix)
}

// prepareRepo copies the sample app into dir with a custom marker message and
// creates a commit with a "minato" remote pointing at the given app.
func prepareRepo(t *testing.T, dir, app, message string) {
	t.Helper()
	src := filepath.Join(repoRoot, "examples", "sample-app")
	for _, f := range []string{"main.go", "go.mod", "Dockerfile"} {
		raw, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	replaceInFile(t, filepath.Join(dir, "main.go"), "Hello from sample-app! (rev 1)", message)

	mustRun(t, "git", "-C", dir, "init", "-q", "-b", "main")
	mustRun(t, "git", "-C", dir, "add", "-A")
	mustRun(t, "git", "-C", dir,
		"-c", "user.email=e2e@minato.local", "-c", "user.name=e2e",
		"commit", "-qm", "init")
	remote := fmt.Sprintf("http://minato.localtest.me%s/git/%s.git", urlSuffix, app)
	mustRun(t, "git", "-C", dir, "remote", "add", "minato", remote)
}

func replaceInFile(t *testing.T, path, old, new string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), old, new)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRun(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func httpGet(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s (%s)", url, resp.Status, body)
	}
	return string(body)
}

// waitForBody polls url until it returns 200 with the wanted substring.
func waitForBody(t *testing.T, url, want string, deadline time.Time) string {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		resp, err := client.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.Contains(string(body), want) {
				return string(body)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not serve %q before the deadline", url, want)
		}
		time.Sleep(time.Second)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
