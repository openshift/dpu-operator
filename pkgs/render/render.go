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

	"github.com/go-logr/logr"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/apply"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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

func applyFromBinData(logger logr.Logger, filePath string, data map[string]string, binData embed.FS, client client.Client, cfg *configv1.DpuOperatorConfig, scheme *runtime.Scheme) error {
	file, err := binData.Open(filepath.Join("bindata", filePath))
	if err != nil {
		return fmt.Errorf("Failed to read file '%s': %v", filePath, err)
	}
	applied, err := ApplyTemplate(file, data)
	if err != nil {
		return fmt.Errorf("Failed to apply template on '%s': %v", filePath, err)
	}
	var obj *unstructured.Unstructured
	err = yaml.NewYAMLOrJSONDecoder(applied, 1024).Decode(&obj)
	if err != nil {
		return err
	}
	if cfg != nil {
		if err := ctrl.SetControllerReference(cfg, obj, scheme); err != nil {
			return err
		}
	}
	logger.Info("Preparing CR", "kind", obj.GetKind())
	if err := apply.ApplyObject(context.TODO(), client, obj); err != nil {
		return fmt.Errorf("failed to apply object %v with err: %v", obj, err)
	}
	return nil
}

func ApplyAllFromBinData(logger logr.Logger, binDataPath string, data map[string]string, binData embed.FS, client client.Client, cfg *configv1.DpuOperatorConfig, scheme *runtime.Scheme) error {
	filePaths, err := BinDataYamlFiles(binDataPath, binData)
	if err != nil {
		return err
	}
	for _, f := range filePaths {
		err = applyFromBinData(logger, f, data, binData, client, cfg, scheme)
		if err != nil {
			return err
		}
	}
	return nil
}
