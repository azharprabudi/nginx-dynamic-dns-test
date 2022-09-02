FROM golang:1.19-buster

WORKDIR /app

COPY go.mod .

RUN go mod download

COPY . .

CMD ["go", "run", "main.go"]