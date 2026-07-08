// Package deploy owns the Kubernetes resources that make up a running app:
// its namespace, Deployment, Service, and Ingress.
package deploy

import (
	"context"
	"fmt"
	"regexp"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	netv1ac "k8s.io/client-go/applyconfigurations/networking/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/KchaiI/slipway/internal/api"
)

const (
	// AppPort is the port apps must listen on, injected as $PORT.
	AppPort = 8080

	fieldManager   = "minato"
	labelApp       = "minato.dev/app"
	labelRelease   = "minato.dev/release"
	labelManagedBy = "app.kubernetes.io/managed-by"
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}$`)

// ValidateAppName enforces the DNS-label-safe app naming contract.
func ValidateAppName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid app name %q: must match %s", name, nameRE)
	}
	return nil
}

// Namespace returns the namespace owning all resources of an app.
func Namespace(app string) string { return "minato-app-" + app }

// Deployer creates and inspects app workloads.
type Deployer struct {
	client kubernetes.Interface
	domain string
	port   int // host port that reaches ingress, for URL rendering
}

func NewDeployer(client kubernetes.Interface, domain string, port int) *Deployer {
	return &Deployer{client: client, domain: domain, port: port}
}

// URL returns the public URL of an app.
func (d *Deployer) URL(app string) string {
	if d.port == 80 {
		return fmt.Sprintf("http://%s.%s", app, d.domain)
	}
	return fmt.Sprintf("http://%s.%s:%d", app, d.domain, d.port)
}

// EnsureNamespace creates the app namespace if it does not exist.
func (d *Deployer) EnsureNamespace(ctx context.Context, app string) error {
	ns := corev1ac.Namespace(Namespace(app)).WithLabels(map[string]string{
		labelApp:       app,
		labelManagedBy: "minato",
	})
	_, err := d.client.CoreV1().Namespaces().Apply(ctx, ns,
		metav1.ApplyOptions{FieldManager: fieldManager, Force: true})
	return err
}

// AppExists reports whether the app namespace exists.
func (d *Deployer) AppExists(ctx context.Context, app string) (bool, error) {
	_, err := d.client.CoreV1().Namespaces().Get(ctx, Namespace(app), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// DeleteNamespace removes the app namespace and everything in it.
func (d *Deployer) DeleteNamespace(ctx context.Context, app string) error {
	err := d.client.CoreV1().Namespaces().Delete(ctx, Namespace(app), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// ListApps returns all apps managed by minato.
func (d *Deployer) ListApps(ctx context.Context) ([]string, error) {
	nss, err := d.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: labelApp,
	})
	if err != nil {
		return nil, err
	}
	apps := make([]string, 0, len(nss.Items))
	for _, ns := range nss.Items {
		if ns.Status.Phase == corev1.NamespaceTerminating {
			continue
		}
		apps = append(apps, ns.Labels[labelApp])
	}
	return apps, nil
}

// Apply creates or updates the app's Deployment, Service, and Ingress for the
// given image and release version. Replicas are intentionally not managed
// here so that scale changes survive deploys.
func (d *Deployer) Apply(ctx context.Context, app, image, version string) error {
	ns := Namespace(app)
	labels := map[string]string{labelApp: app, labelManagedBy: "minato"}
	podLabels := map[string]string{labelApp: app, labelManagedBy: "minato", labelRelease: version}

	container := corev1ac.Container().
		WithName("web").
		WithImage(image).
		WithEnv(corev1ac.EnvVar().WithName("PORT").WithValue(fmt.Sprint(AppPort))).
		WithPorts(corev1ac.ContainerPort().WithName("http").WithContainerPort(AppPort)).
		WithReadinessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().
				WithPath("/").
				WithPort(intstr.FromInt32(AppPort))).
			WithPeriodSeconds(2).
			WithFailureThreshold(30)).
		WithResources(corev1ac.ResourceRequirements().
			WithRequests(corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			}).
			WithLimits(corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			}))

	deployment := appsv1ac.Deployment(app, ns).
		WithLabels(labels).
		WithSpec(appsv1ac.DeploymentSpec().
			WithSelector(metav1ac.LabelSelector().WithMatchLabels(map[string]string{labelApp: app})).
			WithProgressDeadlineSeconds(90).
			WithRevisionHistoryLimit(5).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithLabels(podLabels).
				WithSpec(corev1ac.PodSpec().WithContainers(container))))
	if _, err := d.client.AppsV1().Deployments(ns).Apply(ctx, deployment,
		metav1.ApplyOptions{FieldManager: fieldManager, Force: true}); err != nil {
		return fmt.Errorf("apply deployment: %w", err)
	}

	service := corev1ac.Service(app, ns).
		WithLabels(labels).
		WithSpec(corev1ac.ServiceSpec().
			WithSelector(map[string]string{labelApp: app}).
			WithPorts(corev1ac.ServicePort().
				WithName("http").
				WithPort(80).
				WithTargetPort(intstr.FromInt32(AppPort))))
	if _, err := d.client.CoreV1().Services(ns).Apply(ctx, service,
		metav1.ApplyOptions{FieldManager: fieldManager, Force: true}); err != nil {
		return fmt.Errorf("apply service: %w", err)
	}

	ingress := netv1ac.Ingress(app, ns).
		WithLabels(labels).
		WithSpec(netv1ac.IngressSpec().
			WithIngressClassName("nginx").
			WithRules(netv1ac.IngressRule().
				WithHost(fmt.Sprintf("%s.%s", app, d.domain)).
				WithHTTP(netv1ac.HTTPIngressRuleValue().
					WithPaths(netv1ac.HTTPIngressPath().
						WithPath("/").
						WithPathType(netv1.PathTypePrefix).
						WithBackend(netv1ac.IngressBackend().
							WithService(netv1ac.IngressServiceBackend().
								WithName(app).
								WithPort(netv1ac.ServiceBackendPort().WithNumber(80))))))))
	if _, err := d.client.NetworkingV1().Ingresses(ns).Apply(ctx, ingress,
		metav1.ApplyOptions{FieldManager: fieldManager, Force: true}); err != nil {
		return fmt.Errorf("apply ingress: %w", err)
	}
	return nil
}

// WaitRollout blocks until the app's Deployment has fully rolled out, or
// fails fast when Kubernetes declares the rollout stuck.
func (d *Deployer) WaitRollout(ctx context.Context, app string, timeout time.Duration) error {
	ns := Namespace(app)
	deadline := time.Now().Add(timeout)
	for {
		dep, err := d.client.AppsV1().Deployments(ns).Get(ctx, app, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if done, err := rolloutState(dep); done {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("rollout of %s did not complete within %s", app, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func rolloutState(dep *appsv1.Deployment) (done bool, err error) {
	for _, c := range dep.Status.Conditions {
		if c.Type == appsv1.DeploymentProgressing && c.Status == corev1.ConditionFalse &&
			c.Reason == "ProgressDeadlineExceeded" {
			return true, fmt.Errorf("rollout failed: %s", c.Message)
		}
	}
	want := int32(1)
	if dep.Spec.Replicas != nil {
		want = *dep.Spec.Replicas
	}
	if dep.Generation <= dep.Status.ObservedGeneration &&
		dep.Status.UpdatedReplicas == want &&
		dep.Status.ReadyReplicas == want &&
		dep.Status.AvailableReplicas == want {
		return true, nil
	}
	return false, nil
}

// Scale sets the desired replica count and returns once all replicas are ready.
func (d *Deployer) Scale(ctx context.Context, app string, replicas int32, timeout time.Duration) error {
	ns := Namespace(app)
	dep, err := d.client.AppsV1().Deployments(ns).Get(ctx, app, metav1.GetOptions{})
	if err != nil {
		return err
	}
	dep.Spec.Replicas = &replicas
	if _, err := d.client.AppsV1().Deployments(ns).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("scale deployment: %w", err)
	}
	return d.WaitRollout(ctx, app, timeout)
}

// Status assembles the live state of an app.
func (d *Deployer) Status(ctx context.Context, app string) (*api.AppStatus, error) {
	ns := Namespace(app)
	st := &api.AppStatus{App: api.App{Name: app, URL: d.URL(app)}}

	dep, err := d.client.AppsV1().Deployments(ns).Get(ctx, app, metav1.GetOptions{})
	if err == nil {
		if dep.Spec.Replicas != nil {
			st.Replicas = *dep.Spec.Replicas
		}
		st.ReadyReplicas = dep.Status.ReadyReplicas
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	pods, err := d.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelApp + "=" + app,
	})
	if err != nil {
		return nil, err
	}
	for _, p := range pods.Items {
		info := api.PodInfo{
			Name:    p.Name,
			Phase:   string(p.Status.Phase),
			Release: p.Labels[labelRelease],
		}
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				info.Ready = true
			}
		}
		st.Pods = append(st.Pods, info)
	}
	return st, nil
}
