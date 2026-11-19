package proto

// Меняем metrics.proto, затем запускаем генерацию. Файлы *.pb.go вручную не редактируем.
//go:generate protoc --go_out=. --go_opt=paths=source_relative --go_opt=default_api_level=API_OPAQUE --go-grpc_out=. --go-grpc_opt=paths=source_relative metrics.proto
