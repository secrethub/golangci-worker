run_dev:
	godotenv go run app/cmd/golangci-worker/golangci-worker.go

gen:
	mockgen -package httputils -source ./app/utils/httputils/client.go >./app/utils/httputils/mock.go
	mockgen -package github -source ./app/utils/github/client.go >./app/utils/github/client_mock.go
	mockgen -package linters -source ./app/analyze/linters/linter.go >./app/analyze/linters/linter_mock.go
	mockgen -package fetchers -source ./app/analyze/fetchers/fetcher.go >./app/analyze/fetchers/fetcher_mock.go
	mockgen -package reporters -source ./app/analyze/reporters/reporter.go >./app/analyze/reporters/reporter_mock.go
	mockgen -package executors -source ./app/analyze/executors/executor.go >./app/analyze/executors/executor_mock.go

build:
	go build ./app/cmd/...

install_gometalinter:
	go get gopkg.in/alecthomas/gometalinter.v2
	gometalinter.v2 --install

test:
	go test -v ./...

test_slow:
	SLOW_TESTS_ENABLED=1 go test -v ./app/analyze -run TestAnalyzeSelfRepo
	SLOW_TESTS_ENABLED=1 go test -v ./... -run TestRunLintersInDocker