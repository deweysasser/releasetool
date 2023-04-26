class Cumulus < Formula
  desc "A better AWS (and other cloud) CLI"
  homepage "https://github.com/deweysasser/cumulus"
  version "v0.5.0"

  on_macos do

    if Hardware::CPU.intel?
      url "https://github.com/deweysasser/cumulus/releases/download/v0.5.0/cumulus-darwin-amd64.zip"
      sha256 "d833426e9c5ce8eb7a5acd9a49acb8e31f8ac14ca6f5f3273cadd70a510b5cb8"
    end

    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/deweysasser/cumulus/releases/download/v0.5.0/cumulus-darwin-arm64.zip"
      sha256 "c1dea098f6c597392cbd3e90909a15b8e983a428145b2a1bdaf2550723242d7d"
    end
  end

  on_linux do
    if Hardware::CPU.intel?
      url "https://github.com/deweysasser/cumulus/releases/download/v0.5.0/cumulus-linux-amd64.zip"
      sha256 "52ab5e466452c15d4cee4c9d7e8269a8021429b967a30261ad375438b572d8ae"
    end
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/deweysasser/cumulus/releases/download/v0.5.0/cumulus-linux-arm64.zip"
      sha256 "3195ea54b19f18c1b16d5b35cf815168ac772dc848b5f1efbab506eb972342e0"
    end
  end


  def install
    bin.install "cumulus"
  end
end