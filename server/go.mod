module jmessage

go 1.26.4

require (
	github.com/coder/websocket v1.8.15
	github.com/go-chi/chi/v5 v5.3.1
	github.com/golang-jwt/jwt/v5 v5.3.1
	golang.org/x/crypto v0.53.0
	petdb v0.0.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
)

// The real PetDB engine is the sibling of jMessage under "! Dev".
// NOTE: jMessage\PetDB is a stale reference copy — ../../PetDB (two
// levels up from server/) is intentional; do not "fix" it to ../PetDB.
replace petdb => ../../PetDB
