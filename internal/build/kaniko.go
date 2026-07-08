// Package build turns a pushed git commit into a container image by running
// Kaniko as a Kubernetes Job in the app's namespace.
package build

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	"github.com/KchaiI/slipway/internal/deploy"
)

// Config points the builder at the local registry infrastructure.
type Config struct {
	DataDir        string // contexts live in <DataDir>/contexts/<app>/
	Registry       string // e.g. kind-registry:5000 (plain HTTP)
	RegistryMirror string // docker.io pull-through cache, e.g. kind-registry-mirror:5000
	ContextBaseURL string // where build jobs fetch contexts, e.g. http://minato-server.minato-system.svc/internal/contexts
	KanikoImage    string
	BusyboxImage   string
}

type Builder struct {
	client kubernetes.Interface
	cfg    Config
}

func NewBuilder(client kubernetes.Interface, cfg Config) *Builder {
	return &Builder{client: client, cfg: cfg}
}

// ImageRef is the fully qualified tag a release version is pushed to.
func (b *Builder) ImageRef(app string, version int) string {
	return fmt.Sprintf("%s/%s:v%d", b.cfg.Registry, app, version)
}

// ContextPath is where a release's build context tarball is stored.
func (b *Builder) ContextPath(app string, version int) string {
	return filepath.Join(b.cfg.DataDir, "contexts", app, fmt.Sprintf("v%d.tar.gz", version))
}

// PrepareContext exports the tree of a commit as a tar.gz build context.
func PrepareContext(repoPath, sha, outPath string) error {
	if err := exec.Command("mkdir", "-p", filepath.Dir(outPath)).Run(); err != nil {
		return err
	}
	cmd := exec.Command("git", "-C", repoPath, "archive", "--format=tar.gz", "-o", outPath, sha)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git archive %s: %v: %s", sha, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Run executes the build Job for a release and blocks until it finishes.
// Build output lines are forwarded to progress as they are produced.
func (b *Builder) Run(ctx context.Context, app string, version int, progress func(string)) error {
	ns := deploy.Namespace(app)
	name := fmt.Sprintf("build-%s-v%d", app, version)
	contextURL := fmt.Sprintf("%s/%s/v%d.tar.gz", b.cfg.ContextBaseURL, app, version)

	job := b.jobSpec(app, name, version, contextURL)
	// A leftover job with the same name (e.g. from a retried version) blocks
	// creation; remove it first.
	_ = b.client.BatchV1().Jobs(ns).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
	})
	if err := waitGone(ctx, func() bool {
		_, err := b.client.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}); err != nil {
		return err
	}
	if _, err := b.client.BatchV1().Jobs(ns).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create build job: %w", err)
	}

	logsDone := make(chan struct{})
	streaming := false
	defer func() {
		if streaming {
			<-logsDone
		}
	}()

	deadline := time.Now().Add(5 * time.Minute)
	for {
		j, err := b.client.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if !streaming {
			if pod := b.buildPod(ctx, ns, name); pod != nil && pod.Status.Phase != corev1.PodPending {
				streaming = true
				go func() {
					defer close(logsDone)
					b.streamLogs(ctx, ns, pod.Name, progress)
				}()
			}
		}
		if j.Status.Succeeded > 0 {
			return nil
		}
		if j.Status.Failed > 0 {
			return fmt.Errorf("build job failed: %s", b.failureReason(ctx, ns, name))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("build did not finish within 5 minutes")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func waitGone(ctx context.Context, gone func() bool) error {
	for i := 0; i < 60; i++ {
		if gone() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for previous build job to be deleted")
}

func (b *Builder) buildPod(ctx context.Context, ns, jobName string) *corev1.Pod {
	pods, err := b.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil || len(pods.Items) == 0 {
		return nil
	}
	return &pods.Items[0]
}

// streamLogs follows the kaniko container's output, forwarding each line.
func (b *Builder) streamLogs(ctx context.Context, ns, pod string, progress func(string)) {
	req := b.client.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{
		Container: "kaniko",
		Follow:    true,
	})
	rc, err := req.Stream(ctx)
	if err != nil {
		return
	}
	defer rc.Close()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		progress("       " + sc.Text())
	}
}

// failureReason digs the most useful error out of a failed build pod.
func (b *Builder) failureReason(ctx context.Context, ns, jobName string) string {
	pod := b.buildPod(ctx, ns, jobName)
	if pod == nil {
		return "build pod not found"
	}
	// Init container failure (context download) has no kaniko logs.
	for _, st := range pod.Status.InitContainerStatuses {
		if st.State.Terminated != nil && st.State.Terminated.ExitCode != 0 {
			return fmt.Sprintf("fetching build context failed (%s)", st.State.Terminated.Reason)
		}
	}
	raw, err := b.client.CoreV1().Pods(ns).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: "kaniko",
		TailLines: ptr.To(int64(5)),
	}).DoRaw(ctx)
	if err != nil {
		return "no build logs available"
	}
	return strings.TrimSpace(string(raw))
}

func (b *Builder) jobSpec(app, name string, version int, contextURL string) *batchv1.Job {
	labels := map[string]string{
		"minato.dev/app":               app,
		"minato.dev/build":             fmt.Sprintf("v%d", version),
		"app.kubernetes.io/managed-by": "minato",
	}
	workspace := corev1.VolumeMount{Name: "workspace", MountPath: "/workspace"}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(0)),
			ActiveDeadlineSeconds:   ptr.To(int64(270)),
			TTLSecondsAfterFinished: ptr.To(int32(3600)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{{
						Name:  "fetch-context",
						Image: b.cfg.BusyboxImage,
						Command: []string{"sh", "-c",
							`wget -qO /tmp/ctx.tar.gz "$CONTEXT_URL" && tar -xzf /tmp/ctx.tar.gz -C /workspace`},
						Env:          []corev1.EnvVar{{Name: "CONTEXT_URL", Value: contextURL}},
						VolumeMounts: []corev1.VolumeMount{workspace},
					}},
					Containers: []corev1.Container{{
						Name:  "kaniko",
						Image: b.cfg.KanikoImage,
						Args: []string{
							"--context=dir:///workspace",
							"--dockerfile=Dockerfile",
							"--destination=" + b.ImageRef(app, version),
							"--insecure",
							"--insecure-pull",
							"--registry-mirror=" + b.cfg.RegistryMirror,
							"--insecure-registry=" + b.cfg.Registry,
							"--insecure-registry=" + b.cfg.RegistryMirror,
							"--cache=true",
							"--cache-repo=" + b.cfg.Registry + "/kaniko-cache",
							"--snapshot-mode=redo",
						},
						VolumeMounts: []corev1.VolumeMount{workspace},
					}},
					Volumes: []corev1.Volume{{
						Name:         "workspace",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					}},
				},
			},
		},
	}
}
