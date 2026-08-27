package products

//go:generate go tool oapi-codegen -config cfg.yaml -include-operation-ids productGet,productGetID openapi.json
//go:generate go run ../jsonv2gen client.gen.go
