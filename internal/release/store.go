// Package release manages an app's release history, persisted as a ConfigMap
// in the app's namespace so it survives server restarts.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/KchaiI/slipway/internal/api"
)

const (
	configMapName = "minato-releases"
	historyKey    = "history"

	StatusBuilding   = "building"
	StatusLive       = "live"
	StatusSuperseded = "superseded"
	StatusFailed     = "failed"
)

// History is the full release record of one app.
type History struct {
	Counter  int           `json:"counter"`
	Releases []api.Release `json:"releases"`
}

// Live returns the release currently serving traffic, or nil.
func (h *History) Live() *api.Release {
	for i := range h.Releases {
		if h.Releases[i].Status == StatusLive {
			return &h.Releases[i]
		}
	}
	return nil
}

// Previous returns the newest superseded release, i.e. the rollback target.
func (h *History) Previous() *api.Release {
	var prev *api.Release
	for i := range h.Releases {
		r := &h.Releases[i]
		if r.Status == StatusSuperseded && (prev == nil || r.Version > prev.Version) {
			prev = r
		}
	}
	return prev
}

// Store reads and writes release histories.
type Store struct {
	client kubernetes.Interface
}

func NewStore(client kubernetes.Interface) *Store {
	return &Store{client: client}
}

// Init creates an empty history ConfigMap for a new app.
func (s *Store) Init(ctx context.Context, namespace string) error {
	_, err := s.client.CoreV1().ConfigMaps(namespace).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   configMapName,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "minato"},
		},
		Data: map[string]string{historyKey: `{"counter":0,"releases":[]}`},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// Load returns the history for an app namespace.
func (s *Store) Load(ctx context.Context, namespace string) (*History, error) {
	cm, err := s.client.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("load release history: %w", err)
	}
	var h History
	if err := json.Unmarshal([]byte(cm.Data[historyKey]), &h); err != nil {
		return nil, fmt.Errorf("parse release history: %w", err)
	}
	return &h, nil
}

func (s *Store) save(ctx context.Context, namespace string, h *History) error {
	raw, err := json.Marshal(h)
	if err != nil {
		return err
	}
	cm, err := s.client.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	cm.Data = map[string]string{historyKey: string(raw)}
	_, err = s.client.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

// Allocate reserves the next version number and records it with the given
// initial status.
func (s *Store) Allocate(ctx context.Context, namespace, image, commit, description, status string) (*api.Release, error) {
	h, err := s.Load(ctx, namespace)
	if err != nil {
		return nil, err
	}
	h.Counter++
	rel := api.Release{
		Version:     h.Counter,
		Image:       image,
		Commit:      commit,
		CreatedAt:   time.Now().UTC(),
		Status:      status,
		Description: description,
	}
	h.Releases = append(h.Releases, rel)
	if err := s.save(ctx, namespace, h); err != nil {
		return nil, err
	}
	return &rel, nil
}

// SetStatus updates one release's status. Marking a release live supersedes
// the previously live release.
func (s *Store) SetStatus(ctx context.Context, namespace string, version int, status string) error {
	h, err := s.Load(ctx, namespace)
	if err != nil {
		return err
	}
	for i := range h.Releases {
		r := &h.Releases[i]
		if status == StatusLive && r.Status == StatusLive && r.Version != version {
			r.Status = StatusSuperseded
		}
		if r.Version == version {
			r.Status = status
		}
	}
	return s.save(ctx, namespace, h)
}

// SetImage records the final image reference of a release (e.g. once the
// build pipeline knows the digest-qualified tag).
func (s *Store) SetImage(ctx context.Context, namespace string, version int, image string) error {
	h, err := s.Load(ctx, namespace)
	if err != nil {
		return err
	}
	for i := range h.Releases {
		if h.Releases[i].Version == version {
			h.Releases[i].Image = image
		}
	}
	return s.save(ctx, namespace, h)
}
