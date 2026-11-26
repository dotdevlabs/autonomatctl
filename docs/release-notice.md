The CLI version follows the embedded API contract. When moving from v9.0.0 or
a historical v0.1.0–v0.1.9 installation to contract release v0.0.35, reinstall
after the new release is available. Semver sorts v0.0.35 below those versions.

```sh
brew update
brew reinstall dotdevlabs/tap/autonomatctl
autonomatctl version
```

For manual installations on Linux amd64, download the matching contract tag.

```sh
TAG=v0.0.35
gh release download "$TAG" --repo dotdevlabs/autonomatctl --pattern autonomatctl_linux_amd64.tar.gz --pattern checksums.txt
sha256sum --ignore-missing -c checksums.txt
tar -xzf autonomatctl_linux_amd64.tar.gz autonomatctl
sudo install -m 755 autonomatctl /usr/local/bin/autonomatctl
autonomatctl version
```
