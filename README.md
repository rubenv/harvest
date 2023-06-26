# harvest - Go bindings for Harvest invoicing

[![test](https://github.com/rubenv/harvest/actions/workflows/test.yml/badge.svg)](https://github.com/rubenv/harvest/actions/workflows/test.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/rubenv/harvest.svg)](https://pkg.go.dev/github.com/rubenv/harvest)

https://www.getharvest.com/

## Installation

```
go get github.com/rubenv/harvest
```

Import into your application with:

```go
import "github.com/rubenv/harvest"
```

## Usage

Create an API token on the [Developers page](https://id.getharvest.com/developers) of Harvest ID.

Use your account ID and API token to create a client:

```go
client := harvest.New(123456, "my-token")
```

Check the [documentation](https://godoc.org/github.com/rubenv/harvest) for available methods.

## License

This library is distributed under the [MIT](LICENSE) license.
