class {{.Repo | camelcase}} < Formula
{{- if .PrivateRepo}}
  require_relative "lib/private_access"
{{end }}
  desc "{{.Description}}"
  homepage "https://github.com/{{.Owner}}/{{.Repo}}"
  version "{{.Version}}"

{{- $owner := .Owner }}
{{- $version := .Version }}
{{- $repo := .Repo }}
{{- $private := .PrivateRepo }}

  on_macos do

    if Hardware::CPU.intel?
      {{ range (files . "darwin" "amd64")  -}}
     {{if $private}} url "{{.URL}}", :using => GitHubPrivateRepositoryReleaseDownloadStrategy{{else}} url "https://github.com/{{$owner}}/{{$repo}}/releases/download/{{$version}}/{{.Basename}}"{{end}}
      sha256 "{{.Sha256}}"
      {{- end}}
    end

    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      {{ range (files . "darwin" "arm64") -}}
      {{if $private}} url "{{.URL}}", :using => GitHubPrivateRepositoryReleaseDownloadStrategy{{else}} url "https://github.com/{{$owner}}/{{$repo}}/releases/download/{{$version}}/{{.Basename}}"{{end}}
      sha256 "{{.Sha256}}"
      {{- end}}
    end
  end

  on_linux do
    if Hardware::CPU.intel?
      {{ range (files . "linux" "amd64") -}}
      {{if $private}} url "{{.URL}}", :using => GitHubPrivateRepositoryReleaseDownloadStrategy{{else}} url "https://github.com/{{$owner}}/{{$repo}}/releases/download/{{$version}}/{{.Basename}}"{{end}}
      sha256 "{{.Sha256}}"
      {{- end}}
    end
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      {{ range (files . "linux" "arm64") -}}
      {{if $private}} url "{{.URL}}", :using => GitHubPrivateRepositoryReleaseDownloadStrategy{{else}} url "https://github.com/{{$owner}}/{{$repo}}/releases/download/{{$version}}/{{.Basename}}"{{end}}
      sha256 "{{.Sha256}}"
      {{- end}}
    end
  end


  def install
    bin.install "{{.Repo}}"
  end
end