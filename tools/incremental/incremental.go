package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/template"
)

func main() {
	var dockerfilePath, baseImageURI, outputFilePath string

	flag.StringVar(&dockerfilePath, "dockerfile", "", "Path to the input Dockerfile")
	flag.StringVar(&baseImageURI, "base-uri", "", "Base URI for the new Dockerfile")
	flag.StringVar(&outputFilePath, "output-file", "", "Path to the output Dockerfile")
	flag.Parse()

	if dockerfilePath == "" || baseImageURI == "" || outputFilePath == "" {
		fmt.Println("Usage: -dockerfile <dockerfile> -base-uri <uri> -output-file <file>")
		return
	}

	content, err := os.ReadFile(dockerfilePath)
	if err != nil {
		panic(fmt.Errorf("failed to read input Dockerfile: %v", err))
	}

	newDockerfile, err := processDockerfile(string(content), baseImageURI)
	if err != nil {
		panic(fmt.Errorf("failed to process Dockerfile: %v", err))
	}

	err = os.WriteFile(outputFilePath, []byte(newDockerfile), 0644)
	if err != nil {
		panic(fmt.Errorf("failed to write output Dockerfile: %v", err))
	}

	fmt.Println("New Dockerfile generated successfully:", outputFilePath)
}

const dockerfileTemplate = `
FROM {{.BaseImageURI}}
{{range .CopyCommands}}
{{.}}
{{end}}`

type DockerfileData struct {
	BaseImageURI string
	CopyCommands []string
}

func extractDockerfileParts(dockerfileContent string) (DockerfileData, error) {
	var copyCommands []string

	copyCommands = append(copyCommands, "ARG TARGETARCH")

	lines := strings.Split(dockerfileContent, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "COPY") && strings.Contains(trimmed, "--from=builder") {
			copyCommand := strings.Replace(trimmed, "--from=builder", "", 1)
			copyCommand = strings.Replace(copyCommand, "/workspace/bin/", "bin/", 1)
			copyCommands = append(copyCommands, copyCommand)
		}
	}

	return DockerfileData{
		CopyCommands: copyCommands,
	}, nil
}

func validateDockerfileData(data DockerfileData) error {
	if len(data.CopyCommands) == 0 {
		return fmt.Errorf("no COPY --from=builder commands found in Dockerfile")
	}
	return nil
}

func generateDockerfile(data DockerfileData) (string, error) {
	var newDockerfile strings.Builder
	tmpl, err := template.New("dockerfile").Parse(dockerfileTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to create template: %v", err)
	}
	if err := tmpl.Execute(&newDockerfile, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}
	return newDockerfile.String(), nil
}

func processDockerfile(dockerfileContent, uri string) (string, error) {
	data, err := extractDockerfileParts(dockerfileContent)
	if err != nil {
		return "", err
	}

	if err := validateDockerfileData(data); err != nil {
		return "", err
	}
	data.BaseImageURI = uri
	return generateDockerfile(data)
}
