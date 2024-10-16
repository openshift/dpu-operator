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
	admissionControllersCaCrt string
	templateFile string
	outputFile   string
)

func applyTemplate(registryURL string, admissionControllersCaCrt string, templateBytes []byte) ([]byte, error) {
	templateData := struct {
		RegistryURL string
		AdmissionControllersCaCrt string
	}{
		RegistryURL: registryURL,
		AdmissionControllersCaCrt: admissionControllersCaCrt,
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
	flag.StringVar(&admissionControllersCaCrt, "admissions-controllers-ca-crt", "", "Admission Controller Cert CA Crt")
	flag.StringVar(&templateFile, "template-file", "", "Input template file")
	flag.StringVar(&outputFile, "output-file", "", "Output YAML file")
	flag.Parse()

	if registryURL == "" || admissionControllersCaCrt == "" || templateFile == "" || outputFile == "" {
		fmt.Println("Usage: -registry-url <url> -admissions-controllers-ca-crt <crt> -template-file <file> -output-file <file>")
		return
	}

	templateBytes, err := os.ReadFile(templateFile)
	if err != nil {
		panic(err)
	}

	outputBytes, err := applyTemplate(registryURL, admissionControllersCaCrt, templateBytes)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(outputFile, outputBytes, 0644)
	if err != nil {
		panic(err)
	}
}
