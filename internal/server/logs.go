package server

import (
	"bufio"
	"fmt"
	"net/http"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/KchaiI/slipway/internal/deploy"
)

// handleLogs streams the logs of every pod of an app, fanned into a single
// chunked response with a "[pod] " prefix per line. With follow=1 the stream
// stays open and picks up pods created later (e.g. by scaling) until the
// client disconnects.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	follow := r.URL.Query().Get("follow") == "1"
	ns := deploy.Namespace(app)
	selector := "minato.dev/app=" + app
	pw := newProgressWriter(w)

	if !follow {
		pods, err := s.client.CoreV1().Pods(ns).List(r.Context(), metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return
		}
		for _, p := range pods.Items {
			s.copyPodLogs(r, ns, p.Name, false, pw)
		}
		return
	}

	ctx := r.Context()
	var wg sync.WaitGroup
	streaming := map[string]bool{}
	for {
		pods, err := s.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err == nil {
			for _, p := range pods.Items {
				if streaming[p.Name] || p.Status.Phase != corev1.PodRunning {
					continue
				}
				streaming[p.Name] = true
				wg.Add(1)
				go func(pod string) {
					defer wg.Done()
					s.copyPodLogs(r, ns, pod, true, pw)
				}(p.Name)
			}
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Server) copyPodLogs(r *http.Request, ns, pod string, follow bool, pw *progressWriter) {
	req := s.client.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{
		Container: "web",
		Follow:    follow,
		TailLines: ptr.To(int64(100)),
	})
	rc, err := req.Stream(r.Context())
	if err != nil {
		return
	}
	defer rc.Close()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		pw.Println(fmt.Sprintf("[%s] %s", pod, sc.Text()))
	}
}
