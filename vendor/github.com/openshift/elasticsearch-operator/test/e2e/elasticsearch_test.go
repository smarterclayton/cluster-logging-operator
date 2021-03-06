package e2e

import (
	goctx "context"
	"fmt"
	"github.com/openshift/elasticsearch-operator/pkg/utils"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"testing"
	"time"

	elasticsearch "github.com/openshift/elasticsearch-operator/pkg/apis/elasticsearch/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	retryInterval        = time.Second * 10
	timeout              = time.Second * 300
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5
)

func TestElasticsearch(t *testing.T) {
	elasticsearchList := &elasticsearch.ElasticsearchList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Elasticsearch",
			APIVersion: elasticsearch.SchemeGroupVersion.String(),
		},
	}
	err := framework.AddToFrameworkScheme(elasticsearch.AddToScheme, elasticsearchList)
	if err != nil {
		t.Fatalf("failed to add custom resource scheme to framework: %v", err)
	}
	// run subtests
	t.Run("elasticsearch-group", func(t *testing.T) {
		t.Run("Cluster", ElasticsearchCluster)
	})
}

// Create the secret that would be generated by CLO normally
func createRequiredSecret(f *framework.Framework, ctx *framework.TestCtx) error {
	namespace, err := ctx.GetNamespace()
	if err != nil {
		return fmt.Errorf("Could not get namespace: %v", err)
	}

	elasticsearchSecret := utils.Secret(
		"elasticsearch",
		namespace,
		map[string][]byte{
			"elasticsearch.key": utils.GetFileContents("test/files/elasticsearch.key"),
			"elasticsearch.crt": utils.GetFileContents("test/files/elasticsearch.crt"),
			"logging-es.key":    utils.GetFileContents("test/files/logging-es.key"),
			"logging-es.crt":    utils.GetFileContents("test/files/logging-es.crt"),
			"admin-key":         utils.GetFileContents("test/files/system.admin.key"),
			"admin-cert":        utils.GetFileContents("test/files/system.admin.crt"),
			"admin-ca":          utils.GetFileContents("test/files/ca.crt"),
		},
	)

	err = f.Client.Create(goctx.TODO(), elasticsearchSecret, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	return nil
}

func elasticsearchFullClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) error {
	namespace, err := ctx.GetNamespace()
	if err != nil {
		return fmt.Errorf("Could not get namespace: %v", err)
	}

	cpuValue, _ := resource.ParseQuantity("500m")
	memValue, _ := resource.ParseQuantity("2Gi")

	esNode := elasticsearch.ElasticsearchNode{
		Roles: []elasticsearch.ElasticsearchNodeRole{
			elasticsearch.ElasticsearchRoleClient,
			elasticsearch.ElasticsearchRoleData,
			elasticsearch.ElasticsearchRoleMaster,
		},
		Replicas: int32(1),
		Storage: elasticsearch.ElasticsearchNodeStorageSource{
			EmptyDir: &v1.EmptyDirVolumeSource{},
		},
	}

	// create clusterlogging custom resource
	exampleElasticsearch := &elasticsearch.Elasticsearch{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Elasticsearch",
			APIVersion: elasticsearch.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-elasticsearch",
			Namespace: namespace,
		},
		Spec: elasticsearch.ElasticsearchSpec{
			Spec: elasticsearch.ElasticsearchNodeSpec{
				Image: "openshift/origin-logging-elasticsearch5:latest",
				Resources: v1.ResourceRequirements{
					Limits: v1.ResourceList{
						v1.ResourceCPU:    cpuValue,
						v1.ResourceMemory: memValue,
					},
					Requests: v1.ResourceList{
						v1.ResourceCPU:    cpuValue,
						v1.ResourceMemory: memValue,
					},
				},
			},
			Nodes: []elasticsearch.ElasticsearchNode{
				esNode,
			},
			SecretName: "elasticsearch",
		},
	}
	err = f.Client.Create(goctx.TODO(), exampleElasticsearch, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "example-elasticsearch-clientdatamaster-0-1", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	// Scale up current node
	// then look for example-elasticsearch-clientdatamaster-0-2 and prior node
	exampleElasticsearch.Spec.Nodes[0].Replicas = int32(2)
	err = f.Client.Update(goctx.TODO(), exampleElasticsearch)
	if err != nil {
		return err
	}

	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "example-elasticsearch-clientdatamaster-0-1", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "example-elasticsearch-clientdatamaster-0-2", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	exampleElasticsearch.Spec.Nodes = append(exampleElasticsearch.Spec.Nodes, esNode)
	err = f.Client.Update(goctx.TODO(), exampleElasticsearch)
	if err != nil {
		return err
	}

	// Create another node
	// then look for example-elasticsearch-clientdatamaster-1-1 and prior nodes
	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "example-elasticsearch-clientdatamaster-0-1", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "example-elasticsearch-clientdatamaster-0-2", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "example-elasticsearch-clientdatamaster-1-1", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	// Incorrect scale up and verify we don't see a 4th master created
	exampleElasticsearch.Spec.Nodes[1].Replicas = int32(2)
	err = f.Client.Update(goctx.TODO(), exampleElasticsearch)
	if err != nil {
		return err
	}

	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "example-elasticsearch-clientdatamaster-1-2", 0, retryInterval, time.Second*30)
	if err == nil {
		return fmt.Errorf("Unexpected deployment example-elasticsearch-clientdatamaster-1-2 found.")
	}

	return nil
}

func ElasticsearchCluster(t *testing.T) {
	t.Parallel()
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()
	err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		t.Fatalf("failed to initialize cluster resources: %v", err)
	}
	t.Log("Initialized cluster resources")
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Found namespace: %v", namespace)

	// get global framework variables
	f := framework.Global
	// wait for elasticsearch-operator to be ready
	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "elasticsearch-operator", 1, retryInterval, timeout)
	if err != nil {
		t.Fatal(err)
	}

	if err = createRequiredSecret(f, ctx); err != nil {
		t.Fatal(err)
	}

	if err = elasticsearchFullClusterTest(t, f, ctx); err != nil {
		t.Fatal(err)
	}
}
