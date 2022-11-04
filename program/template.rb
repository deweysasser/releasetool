class {{.Repo}} < Formula
  desc "{{.Description}}"
  homepage "https://github.com/{{.Owner}}/{{.Repo}}"
  version "{{.Version}}"

{{- $owner := .Owner }}
{{- $version := .Version }}
{{- $repo := .Repo }}

  on_macos do

    if Hardware::CPU.intel?
      {{ range (files . "darwin" "amd64")  -}}
      url "https://github.com/{{$owner}}/{{$repo}}/releases/download/{{$version}}/{{.Basename}}"
      sha256 "{{.Sum}}"
      {{- end}}
    end

    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      {{ range (files . "darwin" "arm64") -}}
      url "https://github.com/{{$owner}}/{{$repo}}/releases/download/{{$version}}/{{.Basename}}"
      sha256 "{{.Sum}}"
      {{- end}}
    end
  end

  on_linux do
    if Hardware::CPU.intel?
      {{ range (files . "linux" "amd64") -}}
      url "https://github.com/{{$owner}}/{{$repo}}/releases/download/{{$version}}/{{.Basename}}"
      sha256 "{{.Sum}}"
      {{- end}}
    end
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      {{ range (files . "linux" "arm64") -}}
      url "https://github.com/{{$owner}}/{{$repo}}/releases/download/{{$version}}/{{.Basename}}"
      sha256 "{{.Sum}}"
      {{- end}}
    end
  end


  def install
    bin.install "{{.Repo}}"
  end
end