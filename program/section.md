{{$prefix := .config.Tap -}}
{{range .recipes}}
* [{{.Repo}}](https://github.com/{{.Owner}}/{{.Repo}}) (Version: {{.Version}}) -- {{.Description}}

  ```brew install {{$prefix}}/{{.Repo}}```

{{end}}