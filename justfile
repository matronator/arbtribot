default:
    just --list

build:
    go build -o bin/ .

run:
    bin/arbtribot

test:
    go test .

clean:
    rm -f bin/arbtribot
