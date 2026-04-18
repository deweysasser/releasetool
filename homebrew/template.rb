class {{.ClassName}} < Formula
{{- if .PrivateRepo}}
  require_relative "lib/private_access"
{{end }}
  desc "{{.Description | rubystring}}"
  homepage "https://github.com/{{.Owner | rubystring}}/{{.Repo | rubystring}}"
  version "{{.Version | rubystring}}"

{{- $owner := .Owner }}
{{- $version := .Version }}
{{- $repo := .Repo }}
{{- $private := .PrivateRepo }}

  on_macos do

    if Hardware::CPU.intel?
      {{ range (files . "darwin" "amd64")  -}}
      {{if $private}}url "{{.URL | rubystring}}", :using => GitHubPrivateRepositoryReleaseDownloadStrategy{{else}}url "https://github.com/{{$owner | rubystring}}/{{$repo | rubystring}}/releases/download/{{$version | rubystring}}/{{.Basename | rubystring}}"{{end}}
      sha256 "{{.Sha256 | rubystring}}"
      {{- end}}
    end

    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      {{ range (files . "darwin" "arm64") -}}
      {{if $private}}url "{{.URL | rubystring}}", :using => GitHubPrivateRepositoryReleaseDownloadStrategy{{else}}url "https://github.com/{{$owner | rubystring}}/{{$repo | rubystring}}/releases/download/{{$version | rubystring}}/{{.Basename | rubystring}}"{{end}}
      sha256 "{{.Sha256 | rubystring}}"
      {{- end}}
    end
  end

  on_linux do
    if Hardware::CPU.intel?
      {{ range (files . "linux" "amd64") -}}
      {{if $private}}url "{{.URL | rubystring}}", :using => GitHubPrivateRepositoryReleaseDownloadStrategy{{else}}url "https://github.com/{{$owner | rubystring}}/{{$repo | rubystring}}/releases/download/{{$version | rubystring}}/{{.Basename | rubystring}}"{{end}}
      sha256 "{{.Sha256 | rubystring}}"
      {{- end}}
    end
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      {{ range (files . "linux" "arm64") -}}
      {{if $private}}url "{{.URL | rubystring}}", :using => GitHubPrivateRepositoryReleaseDownloadStrategy{{else}}url "https://github.com/{{$owner | rubystring}}/{{$repo | rubystring}}/releases/download/{{$version | rubystring}}/{{.Basename | rubystring}}"{{end}}
      sha256 "{{.Sha256 | rubystring}}"
      {{- end}}
    end
  end


  def install
    bin.install "{{.Repo | rubystring}}"
  end
end