module github.com/muxvia/muxvia/private/cloud/web-controller

go 1.26.0

require (
	github.com/muxvia/muxvia v0.0.0
	github.com/muxvia/muxvia/private/cloud/control-plane v0.0.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/text v0.34.0 // indirect
)

replace github.com/muxvia/muxvia/private/cloud/control-plane => ../control-plane

replace github.com/muxvia/muxvia => ../../..
