package locations

//go:generate go tool oapi-codegen -config cfg.yaml -include-operation-ids SearchLocations,LocationsGetByID openapi.json
//go:generate go run ../jsonv2gen client.gen.go
