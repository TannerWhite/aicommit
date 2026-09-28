package prompt

import (
	_ "embed"
	"strings"
	"text/template"
)

var (
	//go:embed single.tmpl
	singleSrc string
	//go:embed split.tmpl
	splitSrc string

	singleT = template.Must(template.New("single").Parse(singleSrc))
	splitT  = template.Must(template.New("split").Parse(splitSrc))
)

func render(t *template.Template, data any) string {
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		panic(err) // templates are static; covered by tests
	}
	return b.String()
}

func Single(rules, changes, feedback string) string {
	return render(singleT, map[string]string{"Rules": rules, "Changes": changes, "Feedback": feedback})
}

func Split(rules, changes, validationErrors string) string {
	return render(splitT, map[string]string{"Rules": rules, "Changes": changes, "Errors": validationErrors})
}
