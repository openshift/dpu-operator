package render

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
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
