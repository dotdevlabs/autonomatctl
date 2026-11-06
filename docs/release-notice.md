The CLI version now follows the embedded API contract. When moving from historical
v0.1.0–v0.1.9 installs to a 0.0.x contract release, reinstall: version ordering
otherwise treats the new release as older.

```sh
brew update
brew reinstall dotdevlabs/tap/autonomatctl
autonomatctl version
```

For manual installations on Linux amd64, replace TAG below with the intended
contract release tag (for example, the tag of this release), then run:

```sh
TAG=v0.0.PATCH
gh release download "$TAG" --repo dotdevlabs/autonomatctl --pattern autonomatctl_linux_amd64.tar.gz --pattern checksums.txt
sha256sum --ignore-missing -c checksums.txt
tar -xzf autonomatctl_linux_amd64.tar.gz autonomatctl
sudo install -m 755 autonomatctl /usr/local/bin/autonomatctl
autonomatctl version
```
