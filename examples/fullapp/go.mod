module github.com/arzab/gorock-kit/examples/fullapp

go 1.25.0

require (
	github.com/arzab/gorock-kit/rockbun v0.0.0-00010101000000-000000000000
	github.com/arzab/gorock-kit/rockbus v0.0.0-00010101000000-000000000000
	github.com/arzab/gorock-kit/rockconfig v0.0.0-00010101000000-000000000000
	github.com/arzab/gorock-kit/rockcron v0.0.0-00010101000000-000000000000
	github.com/arzab/gorock-kit/rockengine v0.0.0-00010101000000-000000000000
	github.com/arzab/gorock-kit/rockfiber v0.0.0-00010101000000-000000000000
	github.com/arzab/gorock-kit/rocklog v0.0.0-00010101000000-000000000000
	github.com/arzab/gorock-kit/rockredis v0.0.0-00010101000000-000000000000
	github.com/gofiber/fiber/v2 v2.52.12
)

require (
	github.com/KyleBanks/depth v1.2.1 // indirect
	github.com/PuerkitoBio/purell v1.1.1 // indirect
	github.com/PuerkitoBio/urlesc v0.0.0-20170810143723-de5bf2ad4578 // indirect
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.19.6 // indirect
	github.com/go-openapi/spec v0.20.4 // indirect
	github.com/go-openapi/swag v0.19.15 // indirect
	github.com/gofiber/swagger v1.1.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/philhofer/fwd v1.1.3-0.20240916144458-20a13a1f6b7c // indirect
	github.com/puzpuzpuz/xsync/v3 v3.5.1 // indirect
	github.com/redis/go-redis/v9 v9.18.0 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/swaggo/files/v2 v2.0.2 // indirect
	github.com/swaggo/swag v1.16.4 // indirect
	github.com/tinylib/msgp v1.2.5 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/uptrace/bun v1.2.18 // indirect
	github.com/uptrace/bun/dialect/pgdialect v1.2.18 // indirect
	github.com/uptrace/bun/driver/pgdriver v1.2.18 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.51.0 // indirect
	github.com/valyala/tcplisten v1.0.0 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	go.opentelemetry.io/otel v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	golang.org/x/tools v0.41.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	mellium.im/sasl v0.3.2 // indirect
)

// Local modules: the example always builds against the code in this repo.
replace (
	github.com/arzab/gorock-kit/rockbun => ../../rockbun
	github.com/arzab/gorock-kit/rockbus => ../../rockbus
	github.com/arzab/gorock-kit/rockconfig => ../../rockconfig
	github.com/arzab/gorock-kit/rockcron => ../../rockcron
	github.com/arzab/gorock-kit/rockengine => ../../rockengine
	github.com/arzab/gorock-kit/rockfiber => ../../rockfiber
	github.com/arzab/gorock-kit/rocklog => ../../rocklog
	github.com/arzab/gorock-kit/rockredis => ../../rockredis
)
