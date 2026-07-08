// Package api defines the JSON types exchanged between the minato CLI and
// minato-server.
package api

import "time"

// App is the summary returned by app listing and creation endpoints.
type App struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	GitURL string `json:"gitUrl"`
}

// AppStatus is the detailed state of one app.
type AppStatus struct {
	App
	Replicas      int32     `json:"replicas"`
	ReadyReplicas int32     `json:"readyReplicas"`
	Release       *Release  `json:"release,omitempty"`
	Pods          []PodInfo `json:"pods"`
}

// PodInfo describes one pod backing an app.
type PodInfo struct {
	Name    string `json:"name"`
	Phase   string `json:"phase"`
	Ready   bool   `json:"ready"`
	Release string `json:"release,omitempty"`
}

// Release is one entry in an app's release history.
type Release struct {
	Version     int       `json:"version"`
	Image       string    `json:"image"`
	Commit      string    `json:"commit,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	Status      string    `json:"status"` // building | live | superseded | failed
	Description string    `json:"description"`
}

// CreateAppRequest is the body of POST /v1/apps.
type CreateAppRequest struct {
	Name string `json:"name"`
}

// DeployImageRequest is the body of POST /v1/apps/{app}/deployments.
// It deploys a prebuilt image, bypassing the git build pipeline.
type DeployImageRequest struct {
	Image string `json:"image"`
}

// ScaleRequest is the body of POST /v1/apps/{app}/scale.
type ScaleRequest struct {
	Web int32 `json:"web"`
}

// Error is the JSON error envelope returned on non-2xx responses.
type Error struct {
	Error string `json:"error"`
}
