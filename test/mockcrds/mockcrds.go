package mockcrds

import (
	dockyardsv1 "bitbucket.org/sudosweden/dockyards-backend/pkg/api/v1alpha3"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

var (
	DockyardsNode     = mockCRD(dockyardsv1.NodeKind, "nodes", dockyardsv1.GroupVersion.Group, dockyardsv1.GroupVersion.Version)
	DockyardsCluster  = mockCRD(dockyardsv1.ClusterKind, "clusters", dockyardsv1.GroupVersion.Group, dockyardsv1.GroupVersion.Version)
	DockyardsWorkload = mockCRD(dockyardsv1.WorkloadKind, "workloads", dockyardsv1.GroupVersion.Group, dockyardsv1.GroupVersion.Version)
	ClusterAPIMachine = mockCRD("Machine", "machines", clusterv1.GroupVersion.Group, clusterv1.GroupVersion.Version)
	ClusterAPICluster = mockCRD("Cluster", "clusters", clusterv1.GroupVersion.Group, clusterv1.GroupVersion.Version)

	CRDs = []*apiextensionsv1.CustomResourceDefinition{
		DockyardsNode,
		DockyardsCluster,
		DockyardsWorkload,
		ClusterAPIMachine,
		ClusterAPICluster,
	}
)

func mockCRD(kind, plural, group, version string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: plural + "." + group,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: plural,
				Kind:   kind,
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    version,
					Served:  true,
					Storage: true,
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type:                   "object",
									XPreserveUnknownFields: ptr.To(true),
								},
								"status": {
									Type:                   "object",
									XPreserveUnknownFields: ptr.To(true),
								},
							},
						},
					},
				},
			},
		},
	}
}
