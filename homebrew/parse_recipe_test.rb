class Cumulus < Formula
  desc "Bulk access to multiple AWS clouds"
  homepage "https://github.com/deweysasser/cumulus"
  version "v0.2.0"

  on_macos do

    if Hardware::CPU.intel?
      url "https://github.com/deweysasser/cumulus/releases/download/v0.2.0/cumulus-darwin-amd64.zip"
      sha256 "b30c8a75222adb200c26a95707ce2d4eff7680f1fa91a99691f882863ebdb5ff"
    end

    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/deweysasser/cumulus/releases/download/v0.2.0/cumulus-darwin-arm64.zip"
      sha256 "4043ff8245ffb2e03af501e652be81a7f9ed939960152b84a4f6734915174650"
    end
  end

  on_linux do
    if Hardware::CPU.intel?
      url "https://github.com/deweysasser/cumulus/releases/download/v0.2.0/cumulus-linux-amd64.zip"
      sha256 "6fccd541dc90d99a4f566d85384d51764d23c8b8b837b3de7769f9e0cf9dbb4f"
    end
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/deweysasser/cumulus/releases/download/v0.2.0/cumulus-linux-arm64.zip"
      sha256 "371744ece3466c097558ad731f96276e1c8446e60507b84534dc35129c459a0a"
    end
  end


  def install
    bin.install "cumulus"
  end
end