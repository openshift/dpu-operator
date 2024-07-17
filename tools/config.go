package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"text/template"
)

var (
	registryURL  string
	templateFile string
	outputFile   string
)

func applyTemplate(registryURL string, templateBytes []byte) ([]byte, error) {
	templateData := struct {
		RegistryURL string
	}{
		RegistryURL: registryURL,
	}

	t, err := template.New("example").Parse(string(templateBytes))
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	err = t.Execute(&output, templateData)
	if err != nil {
		return nil, err
	}

	return output.Bytes(), nil
}

func main() {
	flag.StringVar(&registryURL, "registry-url", "", "Registry URL")
	flag.StringVar(&templateFile, "template-file", "", "Input template file")
	flag.StringVar(&outputFile, "output-file", "", "Output YAML file")
	flag.Parse()

	if registryURL == "" || templateFile == "" || outputFile == "" {
		fmt.Println("Usage: -registry-url <url> -template-file <file> -output-file <file>")
		return
	}

	templateBytes, err := os.ReadFile(templateFile)
	if err != nil {
		panic(err)
	}

	outputBytes, err := applyTemplate(registryURL, templateBytes)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(outputFile, outputBytes, 0644)
	if err != nil {
		panic(err)
	}
}
