# Maintainer: Stephanie Wilde-Hobbs <git@stephanie.is>
# Builds the daemon and installs the binary + systemd units.
pkgname=remarkable-manga-git
pkgver=r1.11547e8
pkgrel=1
pkgdesc="MangaDex chapters to reMarkable — send-loop daemon with subscription web UI"
arch=('x86_64')
url="https://github.com/RX14/remarkable-manga"
license=('MIT')
depends=('glibc')
makedepends=('go' 'git')
provides=('remarkable-manga')
source=("git+https://github.com/RX14/remarkable-manga.git")
sha256sums=('SKIP')

pkgver() {
  cd remarkable-manga
  printf "r%s.%s" "$(git rev-list --count HEAD)" "$(git rev-parse --short=7 HEAD)"
}

prepare() {
  cd remarkable-manga
  mkdir -p build/
  export GOPATH="${srcdir}"
  go mod download -modcacherw
}

build() {
  cd remarkable-manga
  export CGO_CPPFLAGS="${CPPFLAGS}"
  export CGO_CFLAGS="${CFLAGS}"
  export CGO_CXXFLAGS="${CXXFLAGS}"
  export CGO_LDFLAGS="${LDFLAGS}"
  export GOPATH="${srcdir}"
  export GOFLAGS="-buildmode=pie -trimpath -ldflags=-linkmode=external -mod=readonly -modcacherw"
  go build -o build/remarkable-manga .
}

check() {
  cd remarkable-manga
  go test ./...
}

package() {
  cd remarkable-manga
  install -Dm755 build/remarkable-manga "$pkgdir"/usr/bin/remarkable-manga
  install -Dm644 remarkable-manga.service "$pkgdir"/usr/lib/systemd/system/remarkable-manga.service
  install -Dm644 remarkable-manga.sysusers "$pkgdir"/usr/lib/sysusers.d/remarkable-manga.conf
}
