package logadapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	loggingv1beta1 "github.com/banzaicloud/logging-operator/pkg/sdk/logging/api/v1beta1"
	"github.com/banzaicloud/logging-operator/pkg/sdk/logging/model/filter"
	opniloggingv1beta1 "github.com/rancher/opni/apis/logging/v1beta1"
	"github.com/rancher/opni/apis/v1beta2"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func BuildLogging(instance interface{}) *loggingv1beta1.Logging {
	var logging *loggingv1beta1.Logging
	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		logging = &loggingv1beta1.Logging{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("opni-%s-%s",
					adapter.GetName(),
					strings.ToLower(string(adapter.Spec.Provider)),
				),
			},
			Spec: loggingv1beta1.LoggingSpec{
				ControlNamespace: controlNamespace(convertSpec(adapter.Spec)),
				FluentbitSpec:    adapter.Spec.FluentConfig.Fluentbit,
				FluentdSpec:      adapter.Spec.FluentConfig.Fluentd,
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, logging)
	case *opniloggingv1beta1.LogAdapter:
		logging = &loggingv1beta1.Logging{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("opni-%s-%s",
					adapter.GetName(),
					strings.ToLower(string(adapter.Spec.Provider)),
				),
			},
			Spec: loggingv1beta1.LoggingSpec{
				ControlNamespace: controlNamespace(adapter.Spec),
				FluentbitSpec:    adapter.Spec.FluentConfig.Fluentbit,
				FluentdSpec:      adapter.Spec.FluentConfig.Fluentd,
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, logging)
	default:
		panic("invalid type")
	}

	return logging
}

func BuildRootLogging(instance interface{}) *loggingv1beta1.Logging {
	var logging *loggingv1beta1.Logging
	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		logging = &loggingv1beta1.Logging{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("opni-%s", adapter.GetName()),
			},
			Spec: loggingv1beta1.LoggingSpec{
				ControlNamespace: controlNamespace(convertSpec(adapter.Spec)),
				FluentbitSpec:    adapter.Spec.RootFluentConfig.Fluentbit,
				FluentdSpec:      adapter.Spec.RootFluentConfig.Fluentd,
				GlobalFilters: []loggingv1beta1.Filter{
					{
						EnhanceK8s: &filter.EnhanceK8s{
							InNamespacePath: []string{`$.kubernetes.namespace_name`},
							InPodPath:       []string{`$.kubernetes.pod_name`},
							APIGroups:       []string{"apps/v1"},
						},
					},
				},
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, logging)
	case *opniloggingv1beta1.LogAdapter:
		logging = &loggingv1beta1.Logging{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("opni-%s", adapter.GetName()),
			},
			Spec: loggingv1beta1.LoggingSpec{
				ControlNamespace: controlNamespace(adapter.Spec),
				FluentbitSpec:    adapter.Spec.RootFluentConfig.Fluentbit,
				FluentdSpec:      adapter.Spec.RootFluentConfig.Fluentd,
				GlobalFilters: []loggingv1beta1.Filter{
					{
						EnhanceK8s: &filter.EnhanceK8s{
							InNamespacePath: []string{`$.kubernetes.namespace_name`},
							InPodPath:       []string{`$.kubernetes.pod_name`},
							APIGroups:       []string{"apps/v1"},
						},
					},
				},
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, logging)
	default:
		panic("invalid type")
	}

	return logging
}

var (
	fluentBitK3sTemplate = template.Must(template.New("fluentbitk3s").Parse(`
[SERVICE]
    Flush             1
    Grace             5
    Daemon            Off
    Log_Level         info
    Coro_Stack_Size   24576
[INPUT]
    Name              systemd
    Tag               k3s
    Path              {{ .Spec.K3S.LogPath }}
    Systemd_Filter    _SYSTEMD_UNIT=k3s.service
    Strip_Underscores On
[FILTER]
    Name              lua
    Match             *
    script            received_at.lua
    call              append
[OUTPUT]
    Name              forward
    Match             *
    Host              opni-{{ .ObjectMeta.Name }}-fluentd.{{ .Spec.OpniCluster.Namespace }}.svc
    Port              24240
    Retry_Limit       False
`))

	receivedAtLua = `
function append(tag, timestamp, record)
    new_record = record
    new_record["timestamp"] = os.date("!%Y-%m-%dT%TZ",t)
    return 1, timestamp, new_record
end
`
	fluentBitRKETemplate = template.Must(template.New("fluentbitrke").Parse(`
[SERVICE]
    Log_Level         {{ .Spec.RKE.LogLevel }}
    Parsers_File      parsers.conf
[INPUT]
    Tag               rke
    Name              tail
    Path_Key          filename
    Parser            docker
    DB                /tail-db/tail-containers-state.db
    Mem_Buf_Limit     5MB
    Path              /var/lib/rancher/rke/log/*.log
[OUTPUT]
    Name              forward
    Match             *
    Host              opni-{{ .ObjectMeta.Name }}-fluentd.{{ .Spec.OpniCluster.Namespace }}.svc
    Port              24240
    Retry_Limit       False
`))
	fluentBitRKE2Template = template.Must(template.New("fluentbitrke2").Parse(`
[SERVICE]
    Flush             1
    Grace             5
    Daemon            Off
    Log_Level         info
    Coro_Stack_Size   24576
[INPUT]
    Name              systemd
    Tag               rke2
    Path              {{ .Spec.RKE2.LogPath }}
    Systemd_Filter    _SYSTEMD_UNIT=rke2-server.service
    Systemd_Filter    _SYSTEMD_UNIT=rke2-agent.service
    Strip_Underscores On
[INPUT]
    Tag               rke2
    Name              tail
    Path_Key          filename
    DB                /tail-db/tail-containers-state.db
    Mem_Buf_Limit     5MB
    Path              /var/lib/rancher/rke2/agent/logs/kubelet.log
[FILTER]
    Name              lua
    Match             *
    script            received_at.lua
    call              append
[OUTPUT]
    Name              forward
    Match             *
    Host              opni-{{ .ObjectMeta.Name }}-fluentd.{{ .Spec.OpniCluster.Namespace }}.svc
    Port              24240
    Retry_Limit       False
`))
)

func BuildK3SConfig(instance interface{}) *corev1.ConfigMap {
	var (
		buffer    bytes.Buffer
		configmap *corev1.ConfigMap
	)
	// If the opni cluster is nil, copy the object to use for generating the config
	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		if adapter.Spec.OpniCluster == nil {
			copy := adapter.DeepCopy()
			copy.Spec.OpniCluster = &v1beta2.OpniClusterNameSpec{
				Namespace: "opni-system",
			}
			fluentBitK3sTemplate.Execute(&buffer, copy)
		} else {
			fluentBitK3sTemplate.Execute(&buffer, adapter)
		}

		configmap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("opni-%s-k3s", adapter.GetName()),
				Namespace: controlNamespace(convertSpec(adapter.Spec)),
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, configmap)
	case *opniloggingv1beta1.LogAdapter:
		if adapter.Spec.OpniCluster == nil {
			copy := adapter.DeepCopy()
			copy.Spec.OpniCluster = &opniloggingv1beta1.OpniClusterNameSpec{
				Namespace: "opni-system",
			}
			fluentBitK3sTemplate.Execute(&buffer, copy)
		} else {
			fluentBitK3sTemplate.Execute(&buffer, adapter)
		}

		configmap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("opni-%s-k3s", adapter.GetName()),
				Namespace: controlNamespace(adapter.Spec),
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, configmap)
	}

	configmap.Data = map[string]string{
		"fluent-bit.conf": buffer.String(),
		"received_at.lua": receivedAtLua,
	}

	return configmap
}

func BuildK3SJournaldAggregator(instance interface{}) *appsv1.DaemonSet {
	var daemonset *appsv1.DaemonSet

	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		name := fmt.Sprintf("opni-%s-k3s-journald-aggregator", adapter.GetName())
		podLabels := map[string]string{
			"name": name,
		}
		daemonset = &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(convertSpec(adapter.Spec)),
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: podLabels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name:   name,
						Labels: podLabels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "fluentbit",
								Image: adapter.Spec.FluentConfig.Fluentbit.Image.RepositoryWithTag(),
								SecurityContext: &corev1.SecurityContext{
									SELinuxOptions: &corev1.SELinuxOptions{
										Type: "rke_logreader_t",
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "config",
										MountPath: "/fluent-bit/etc/",
									},
									{
										Name:      "journal",
										MountPath: adapter.Spec.K3S.LogPath,
										ReadOnly:  true,
									},
									{
										Name:      "machine-id",
										MountPath: "/etc/machine-id",
										ReadOnly:  true,
									},
								},
							},
						},
						ServiceAccountName: name,
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: fmt.Sprintf("opni-%s-k3s", adapter.GetName()),
										},
									},
								},
							},
							{
								Name: "journal",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: adapter.Spec.K3S.LogPath,
									},
								},
							},
							{
								Name: "machine-id",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/etc/machine-id",
									},
								},
							},
						},
					},
				},
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, daemonset)
	case *opniloggingv1beta1.LogAdapter:
		name := fmt.Sprintf("opni-%s-k3s-journald-aggregator", adapter.GetName())
		podLabels := map[string]string{
			"name": name,
		}
		daemonset = &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(adapter.Spec),
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: podLabels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name:   name,
						Labels: podLabels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "fluentbit",
								Image: adapter.Spec.FluentConfig.Fluentbit.Image.RepositoryWithTag(),
								SecurityContext: &corev1.SecurityContext{
									SELinuxOptions: &corev1.SELinuxOptions{
										Type: "rke_logreader_t",
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "config",
										MountPath: "/fluent-bit/etc/",
									},
									{
										Name:      "journal",
										MountPath: adapter.Spec.K3S.LogPath,
										ReadOnly:  true,
									},
									{
										Name:      "machine-id",
										MountPath: "/etc/machine-id",
										ReadOnly:  true,
									},
								},
							},
						},
						ServiceAccountName: name,
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: fmt.Sprintf("opni-%s-k3s", adapter.GetName()),
										},
									},
								},
							},
							{
								Name: "journal",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: adapter.Spec.K3S.LogPath,
									},
								},
							},
							{
								Name: "machine-id",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/etc/machine-id",
									},
								},
							},
						},
					},
				},
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, daemonset)
	default:
		panic("invalid type")
	}
	return daemonset
}

func BuildK3SServiceAccount(instance interface{}) *corev1.ServiceAccount {
	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		name := fmt.Sprintf("opni-%s-k3s-journald-aggregator", adapter.GetName())
		return &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(convertSpec(adapter.Spec)),
			},
		}
	case *opniloggingv1beta1.LogAdapter:
		name := fmt.Sprintf("opni-%s-k3s-journald-aggregator", adapter.GetName())
		return &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(adapter.Spec),
			},
		}
	default:
		panic("invalid type")
	}
}

func BuildRKEConfig(instance interface{}) *corev1.ConfigMap {
	var buffer bytes.Buffer
	var configmap *corev1.ConfigMap
	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		// If the opni cluster is nil, copy the object to use for generating the config
		if adapter.Spec.OpniCluster == nil {
			copy := adapter.DeepCopy()
			copy.Spec.OpniCluster = &v1beta2.OpniClusterNameSpec{
				Namespace: "opni-system",
			}
			fluentBitRKETemplate.Execute(&buffer, copy)
		} else {
			fluentBitRKETemplate.Execute(&buffer, adapter)
		}
		configmap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("opni-%s-rke", adapter.GetName()),
				Namespace: controlNamespace(convertSpec(adapter.Spec)),
			},
			Data: map[string]string{
				"fluent-bit.conf": buffer.String(),
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, configmap)
	case *opniloggingv1beta1.LogAdapter:
		if adapter.Spec.OpniCluster == nil {
			copy := adapter.DeepCopy()
			copy.Spec.OpniCluster = &opniloggingv1beta1.OpniClusterNameSpec{
				Namespace: "opni-system",
			}
			fluentBitRKETemplate.Execute(&buffer, copy)
		} else {
			fluentBitRKETemplate.Execute(&buffer, adapter)
		}
		configmap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("opni-%s-rke", adapter.GetName()),
				Namespace: controlNamespace(adapter.Spec),
			},
			Data: map[string]string{
				"fluent-bit.conf": buffer.String(),
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, configmap)
	default:
		panic("invalid type")
	}

	return configmap
}

func BuildRKEAggregator(instance interface{}) *appsv1.DaemonSet {
	var daemonset *appsv1.DaemonSet

	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		name := fmt.Sprintf("opni-%s-rke-aggregator", adapter.GetName())
		podLabels := map[string]string{
			"name": name,
		}
		directoryOrCreate := corev1.HostPathDirectoryOrCreate
		daemonset = &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(convertSpec(adapter.Spec)),
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: podLabels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name:   name,
						Labels: podLabels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "fluentbit",
								Image: adapter.Spec.FluentConfig.Fluentbit.Image.RepositoryWithTag(),
								SecurityContext: &corev1.SecurityContext{
									SELinuxOptions: &corev1.SELinuxOptions{
										Type: "rke_logreader_t",
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										MountPath: "/var/lib/rancher/rke/log/",
										Name:      "indir",
									},
									{
										MountPath: adapter.Spec.ContainerLogDir,
										Name:      "containers",
									},
									{
										MountPath: "/tail-db",
										Name:      "positiondb",
									},
									{
										MountPath: "/fluent-bit/etc/fluent-bit.conf",
										Name:      "config",
										SubPath:   "fluent-bit.conf",
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "indir",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/var/lib/rancher/rke/log/",
										Type: &directoryOrCreate,
									},
								},
							},
							{
								Name: "containers",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: adapter.Spec.ContainerLogDir,
										Type: &directoryOrCreate,
									},
								},
							},
							{
								Name: "positiondb",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: fmt.Sprintf("opni-%s-rke", adapter.GetName()),
										},
									},
								},
							},
						},
						ServiceAccountName: name,
						Tolerations: []corev1.Toleration{
							{
								Key:    "node-role.kubernetes.io/controlplane",
								Value:  "true",
								Effect: corev1.TaintEffectNoSchedule,
							},
							{
								Key:    "node-role.kubernetes.io/etcd",
								Value:  "true",
								Effect: corev1.TaintEffectNoExecute,
							},
						},
					},
				},
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, daemonset)
	case *opniloggingv1beta1.LogAdapter:
		name := fmt.Sprintf("opni-%s-rke-aggregator", adapter.GetName())
		podLabels := map[string]string{
			"name": name,
		}
		directoryOrCreate := corev1.HostPathDirectoryOrCreate
		daemonset = &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(adapter.Spec),
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: podLabels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name:   name,
						Labels: podLabels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "fluentbit",
								Image: adapter.Spec.FluentConfig.Fluentbit.Image.RepositoryWithTag(),
								SecurityContext: &corev1.SecurityContext{
									SELinuxOptions: &corev1.SELinuxOptions{
										Type: "rke_logreader_t",
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										MountPath: "/var/lib/rancher/rke/log/",
										Name:      "indir",
									},
									{
										MountPath: adapter.Spec.ContainerLogDir,
										Name:      "containers",
									},
									{
										MountPath: "/tail-db",
										Name:      "positiondb",
									},
									{
										MountPath: "/fluent-bit/etc/fluent-bit.conf",
										Name:      "config",
										SubPath:   "fluent-bit.conf",
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "indir",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/var/lib/rancher/rke/log/",
										Type: &directoryOrCreate,
									},
								},
							},
							{
								Name: "containers",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: adapter.Spec.ContainerLogDir,
										Type: &directoryOrCreate,
									},
								},
							},
							{
								Name: "positiondb",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: fmt.Sprintf("opni-%s-rke", adapter.GetName()),
										},
									},
								},
							},
						},
						ServiceAccountName: name,
						Tolerations: []corev1.Toleration{
							{
								Key:    "node-role.kubernetes.io/controlplane",
								Value:  "true",
								Effect: corev1.TaintEffectNoSchedule,
							},
							{
								Key:    "node-role.kubernetes.io/etcd",
								Value:  "true",
								Effect: corev1.TaintEffectNoExecute,
							},
						},
					},
				},
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, daemonset)
	default:
		panic("invalid type")
	}
	return daemonset
}

func BuildRKEServiceAccount(instance interface{}) *corev1.ServiceAccount {
	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		name := fmt.Sprintf("opni-%s-rke-aggregator", adapter.GetName())
		return &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(convertSpec(adapter.Spec)),
			},
		}
	case *opniloggingv1beta1.LogAdapter:
		name := fmt.Sprintf("opni-%s-rke-aggregator", adapter.GetName())
		return &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(adapter.Spec),
			},
		}
	default:
		panic("invalid type")
	}
}

func BuildRKE2Config(instance interface{}) *corev1.ConfigMap {
	var buffer bytes.Buffer
	var configmap *corev1.ConfigMap

	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		// If the opni cluster is nil, copy the object to use for generating the config
		if adapter.Spec.OpniCluster == nil {
			copy := adapter.DeepCopy()
			copy.Spec.OpniCluster = &v1beta2.OpniClusterNameSpec{
				Namespace: "opni-system",
			}
			fluentBitRKE2Template.Execute(&buffer, copy)
		} else {
			fluentBitRKE2Template.Execute(&buffer, adapter)
		}

		configmap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("opni-%s-rke2", adapter.GetName()),
				Namespace: controlNamespace(convertSpec(adapter.Spec)),
			},
			Data: map[string]string{
				"fluent-bit.conf": buffer.String(),
				"received_at.lua": receivedAtLua,
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, configmap)
	case *opniloggingv1beta1.LogAdapter:
		if adapter.Spec.OpniCluster == nil {
			copy := adapter.DeepCopy()
			copy.Spec.OpniCluster = &opniloggingv1beta1.OpniClusterNameSpec{
				Namespace: "opni-system",
			}
			fluentBitRKE2Template.Execute(&buffer, copy)
		} else {
			fluentBitRKE2Template.Execute(&buffer, adapter)
		}

		configmap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("opni-%s-rke2", adapter.GetName()),
				Namespace: controlNamespace(adapter.Spec),
			},
			Data: map[string]string{
				"fluent-bit.conf": buffer.String(),
				"received_at.lua": receivedAtLua,
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, configmap)
	default:
		panic("invalid type")
	}
	return configmap
}

func BuildRKE2JournaldAggregator(instance interface{}) *appsv1.DaemonSet {
	var daemonset *appsv1.DaemonSet
	directoryOrCreate := corev1.HostPathDirectoryOrCreate

	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		name := fmt.Sprintf("opni-%s-rke2-journald-aggregator", adapter.GetName())
		podLabels := map[string]string{
			"name": name,
		}
		daemonset = &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(convertSpec(adapter.Spec)),
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: podLabels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name:   name,
						Labels: podLabels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "fluentbit",
								Image: adapter.Spec.FluentConfig.Fluentbit.Image.RepositoryWithTag(),
								SecurityContext: &corev1.SecurityContext{
									SELinuxOptions: &corev1.SELinuxOptions{
										Type: "rke_logreader_t",
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "config",
										MountPath: "/fluent-bit/etc/",
									},
									{
										Name:      "journal",
										MountPath: adapter.Spec.RKE2.LogPath,
										ReadOnly:  true,
									},
									{
										Name:      "machine-id",
										MountPath: "/etc/machine-id",
										ReadOnly:  true,
									},
									{
										MountPath: "/tail-db",
										Name:      "positiondb",
									},
									{
										MountPath: "/var/lib/rancher/rke2/agent/logs/",
										Name:      "indir",
									},
								},
							},
						},
						ServiceAccountName: name,
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: fmt.Sprintf("opni-%s-rke2", adapter.GetName()),
										},
									},
								},
							},
							{
								Name: "journal",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: adapter.Spec.RKE2.LogPath,
									},
								},
							},
							{
								Name: "machine-id",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/etc/machine-id",
									},
								},
							},
							{
								Name: "positiondb",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
							{
								Name: "indir",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/var/lib/rancher/rke2/agent/logs/",
										Type: &directoryOrCreate,
									},
								},
							},
						},
					},
				},
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, daemonset)
	case *opniloggingv1beta1.LogAdapter:
		name := fmt.Sprintf("opni-%s-rke2-journald-aggregator", adapter.GetName())
		podLabels := map[string]string{
			"name": name,
		}
		daemonset = &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(adapter.Spec),
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: podLabels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name:   name,
						Labels: podLabels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "fluentbit",
								Image: adapter.Spec.FluentConfig.Fluentbit.Image.RepositoryWithTag(),
								SecurityContext: &corev1.SecurityContext{
									SELinuxOptions: &corev1.SELinuxOptions{
										Type: "rke_logreader_t",
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "config",
										MountPath: "/fluent-bit/etc/",
									},
									{
										Name:      "journal",
										MountPath: adapter.Spec.RKE2.LogPath,
										ReadOnly:  true,
									},
									{
										Name:      "machine-id",
										MountPath: "/etc/machine-id",
										ReadOnly:  true,
									},
									{
										MountPath: "/tail-db",
										Name:      "positiondb",
									},
									{
										MountPath: "/var/lib/rancher/rke2/agent/logs/",
										Name:      "indir",
									},
								},
							},
						},
						ServiceAccountName: name,
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: fmt.Sprintf("opni-%s-rke2", adapter.GetName()),
										},
									},
								},
							},
							{
								Name: "journal",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: adapter.Spec.RKE2.LogPath,
									},
								},
							},
							{
								Name: "machine-id",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/etc/machine-id",
									},
								},
							},
							{
								Name: "positiondb",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
							{
								Name: "indir",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/var/lib/rancher/rke2/agent/logs/",
										Type: &directoryOrCreate,
									},
								},
							},
						},
					},
				},
			},
		}
		setOwnerReference(adapter.ObjectMeta, adapter.TypeMeta, daemonset)
	default:
		panic("invalid type")
	}
	return daemonset
}

func BuildRKE2ServiceAccount(instance interface{}) *corev1.ServiceAccount {
	switch adapter := instance.(type) {
	case *v1beta2.LogAdapter:
		name := fmt.Sprintf("opni-%s-rke2-journald-aggregator", adapter.GetName())
		return &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(convertSpec(adapter.Spec)),
			},
		}
	case *opniloggingv1beta1.LogAdapter:
		name := fmt.Sprintf("opni-%s-rke2-journald-aggregator", adapter.GetName())
		return &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: controlNamespace(adapter.Spec),
			},
		}
	default:
		panic("invalid type")
	}
}

func setOwnerReference(oMeta metav1.ObjectMeta, tMeta metav1.TypeMeta, object client.Object) {
	object.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         tMeta.APIVersion,
			Kind:               tMeta.Kind,
			Name:               oMeta.Name,
			UID:                oMeta.UID,
			Controller:         lo.ToPtr(true),
			BlockOwnerDeletion: lo.ToPtr(true),
		},
	})
}

func controlNamespace(spec opniloggingv1beta1.LogAdapterSpec) string {
	if spec.OpniCluster == nil {
		if spec.ControlNamespace == nil {
			return "opni-system"
		}
		return *spec.ControlNamespace
	}
	return spec.OpniCluster.Namespace
}

func convertSpec(input v1beta2.LogAdapterSpec) opniloggingv1beta1.LogAdapterSpec {
	data, err := json.Marshal(input)
	if err != nil {
		panic(err)
	}
	retSpec := opniloggingv1beta1.LogAdapterSpec{}
	err = json.Unmarshal(data, &retSpec)
	if err != nil {
		panic(err)
	}
	return retSpec
}
