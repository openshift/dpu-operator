package render

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/go-logr/logr"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/apply"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ApplyTemplate(reader io.Reader, vars map[string]string) (io.Reader, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	t, err := template.New("template").Option("missingkey=error").Parse(string(contents))
	if err != nil {
		return nil, fmt.Errorf("Failed to parse yaml through template: %v", err)
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, vars)
	if err != nil {
		return nil, fmt.Errorf("Failed to Execute template on buffer: %v", err)
	}
	return bytes.NewReader(buf.Bytes()), nil
}

func BinDataYamlFiles(dirPath string, binData embed.FS) ([]string, error) {
	var yamlFileDescriptors []string

	dir, err := binData.ReadDir(filepath.Join("bindata", dirPath))
	if err != nil {
		return nil, err
	}

	for _, f := range dir {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".yaml") {
			yamlFileDescriptors = append(yamlFileDescriptors, filepath.Join(dirPath, f.Name()))
		}
	}

	sort.Strings(yamlFileDescriptors)
	return yamlFileDescriptors, nil
}

func applyObjectFromBinData(logger logr.Logger, filePath string, data map[string]string, binData embed.FS, c client.Client, owner client.Object) (client.Object, error) {
	file, err := binData.Open(filepath.Join("bindata", filePath))
	if err != nil {
		return nil, fmt.Errorf("Failed to read file '%s': %v", filePath, err)
	}
	applied, err := ApplyTemplate(file, data)
	if err != nil {
		return nil, fmt.Errorf("Failed to apply template on '%s': %v", filePath, err)
	}
	var obj *unstructured.Unstructured
	err = yaml.NewYAMLOrJSONDecoder(applied, 1024).Decode(&obj)
	if err != nil {
		return nil, err
	}
	if owner != nil {
		if err := ctrl.SetControllerReference(owner, obj, c.Scheme()); err != nil {
			return nil, err
		}
	}
	logger.Info("Preparing CR", "kind", obj.GetKind())

	// Check if resource already exists first
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	err = c.Get(context.TODO(), client.ObjectKey{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}, existing)

	if err == nil {
		// Resource exists, return it without trying to update
		logger.Info("Resource already exists, skipping update", "kind", obj.GetKind(), "name", obj.GetName())
		return existing, nil
	} else if !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to check existing resource", "kind", obj.GetKind(), "name", obj.GetName())
		return nil, fmt.Errorf("failed to check existing resource: %v", err)
	}

	logger.Info("Resource does not exist, creating it", "kind", obj.GetKind(), "name", obj.GetName())

	// Resource doesn't exist, apply it
	if err := apply.ApplyObject(context.TODO(), c, obj); err != nil {
		// When resources (for example the VSP) is deployed multiple times in the case of 1 cluster,
		// we want to ignore already exists errors. Also handle conflict errors when resources are
		// created concurrently by multiple daemons (e.g. errors which occur when the resource has been modified since last read)
		if apierrors.IsAlreadyExists(err) {
			logger.Info("Resource already exists, but will return for tracking", "kind", obj.GetKind(), "name", obj.GetName())
			return obj, nil
		}

		if apierrors.IsConflict(err) {
			logger.Info("Resource conflict detected, but will return for tracking", "kind", obj.GetKind(), "name", obj.GetName())
			return obj, nil
		}
		return nil, fmt.Errorf("failed to apply object %v with err: %v", obj, err)
	}
	return obj, nil
}

func ApplyAllFromBinData(logger logr.Logger, binDataPath string, data map[string]string, binData embed.FS, c client.Client, owner client.Object) error {
	return ApplyAllFromBinDataWithRenderer(logger, binDataPath, data, binData, c, owner, nil)
}

// ApplyAllFromBinDataWithRenderer applies all resources and optionally tracks them in the provided renderer
func ApplyAllFromBinDataWithRenderer(logger logr.Logger, binDataPath string, data map[string]string, binData embed.FS, c client.Client, owner client.Object, renderer *ResourceRenderer) error {
	filePaths, err := BinDataYamlFiles(binDataPath, binData)
	if err != nil {
		return err
	}

	for _, f := range filePaths {
		obj, err := applyObjectFromBinData(logger, f, data, binData, c, owner)
		if err != nil {
			return err
		}
		if obj != nil {
			// Automatically track in renderer if provided
			if renderer != nil {
				if unstruct, ok := obj.(*unstructured.Unstructured); ok {
					renderer.trackResource(unstruct, logger)
				} else {
					logger.Info("Skipping non-unstructured object for tracking", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName())
				}
			}
		}
	}
	return nil
}

// ResourceRenderer provides an interface for rendering and cleaning up unstructured Kubernetes resources
type ResourceRenderer struct {
	parentKey string                                // the key identifying this parent resource
	resources map[string]*unstructured.Unstructured // resource key -> resource
	order     []string                              // ordered resource keys
}

// NewResourceRenderer creates a new resource renderer for a specific parent key
func NewResourceRenderer(parentKey string) *ResourceRenderer {
	return &ResourceRenderer{
		parentKey: parentKey,
		resources: make(map[string]*unstructured.Unstructured),
		order:     make([]string, 0),
	}
}

// trackResource adds a resource to the renderer with automatic deduplication and key derivation
func (rr *ResourceRenderer) trackResource(resource *unstructured.Unstructured, logger logr.Logger) {
	// Derive resource key from the object itself
	resourceKey := fmt.Sprintf("%s/%s/%s", resource.GetKind(), resource.GetNamespace(), resource.GetName())

	// Only track if not already tracked (automatic deduplication)
	if _, exists := rr.resources[resourceKey]; !exists {
		rr.resources[resourceKey] = resource
		rr.order = append(rr.order, resourceKey)
		logger.Info("Tracked resource", "parent", rr.parentKey, "resourceKey", resourceKey)
	} else {
		logger.V(1).Info("Resource already tracked, skipping", "parent", rr.parentKey, "resourceKey", resourceKey)
	}
}

// CleanupResourcesInReverseOrder performs LIFO cleanup of rendered resources
func (rr *ResourceRenderer) ApplyAllFromBinData(logger logr.Logger, binDataPath string, data map[string]string, binData embed.FS, c client.Client, owner client.Object) error {
	return ApplyAllFromBinDataWithRenderer(logger, binDataPath, data, binData, c, owner, rr)
}

func (rr *ResourceRenderer) CleanupResourcesInReverseOrder(ctx context.Context, c client.Client, logger logr.Logger) error {
	if len(rr.order) == 0 {
		logger.Info("No resources to clean up", "parent", rr.parentKey)
		return nil
	}

	var cleanupErrors []error

	logger.Info("Starting reverse-order cleanup", "parent", rr.parentKey, "resourceCount", len(rr.order))

	// Cleanup in reverse order (LIFO)
	for i := len(rr.order) - 1; i >= 0; i-- {
		resourceKey := rr.order[i]
		resource, exists := rr.resources[resourceKey]
		if !exists {
			logger.Info("Resource not found in map, skipping", "resourceKey", resourceKey)
			continue
		}

		logger.Info("Deleting resource", "parent", rr.parentKey, "resourceKey", resourceKey, "name", resource.GetName(), "namespace", resource.GetNamespace())

		// Try to delete the resource
		err := c.Delete(ctx, resource)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("Resource already deleted", "resourceKey", resourceKey)
			} else {
				logger.Error(err, "Failed to delete resource", "resourceKey", resourceKey)
				cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to delete %s: %v", resourceKey, err))
			}
		} else {
			logger.Info("Initiated deletion of resource", "resourceKey", resourceKey)

			// Wait for the resource to be actually deleted
			waitErr := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 300*time.Second, true, func(ctx context.Context) (bool, error) {
				checkObj := resource.DeepCopyObject().(client.Object)
				err := c.Get(ctx, types.NamespacedName{Name: resource.GetName(), Namespace: resource.GetNamespace()}, checkObj)
				if apierrors.IsNotFound(err) {
					return true, nil // Resource is deleted
				}
				if err != nil {
					return false, err // Unexpected error
				}
				return false, nil // Resource still exists
			})

			if waitErr != nil {
				logger.Error(waitErr, "Timeout waiting for resource deletion", "resourceKey", resourceKey)
				cleanupErrors = append(cleanupErrors, fmt.Errorf("timeout waiting for deletion of %s: %v", resourceKey, waitErr))
			} else {
				logger.Info("Successfully deleted resource", "resourceKey", resourceKey)
			}
		}
	}

	// Clear the tracking after cleanup attempt
	rr.resources = make(map[string]*unstructured.Unstructured)
	rr.order = make([]string, 0)

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("cleanup errors: %v", cleanupErrors)
	}

	logger.Info("Successfully completed reverse-order cleanup", "parent", rr.parentKey)
	return nil
}
