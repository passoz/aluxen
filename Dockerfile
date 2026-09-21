# Imagem do Aluxen (Go): binário estático em distroless — sem Chromium, sem
# runtime de linguagem, sem shell. A persistência vive no volume montado em /data.
FROM golang:1.27 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/aluxen ./cmd/app

FROM gcr.io/distroless/static-debian12

COPY --from=build /out/aluxen /aluxen

ENV APP_ENV=production \
    HOST=0.0.0.0 \
    PORT=3000 \
    DB_DIR=/data \
    LOG_FORMAT=json \
    TZ=America/Sao_Paulo
LABEL aluxen="go" \
      org.opencontainers.image.title="aluxen:go"


EXPOSE 3000

# Identificação do build (injetada pelo workflow de publicação): o painel exibe
# estes valores no modal de Informações para identificar a imagem em execução.
ARG ALUXEN_BUILD_COMMIT=""
ARG ALUXEN_BUILD_COMMIT_SHORT=""
ARG ALUXEN_BUILD_REF=""
ARG ALUXEN_BUILD_TIME=""
ARG ALUXEN_BUILD_IMAGE=""
ARG ALUXEN_BUILD_RUN_URL=""
ARG ALUXEN_BUILD_NUMBER=""
ENV ALUXEN_BUILD_COMMIT=$ALUXEN_BUILD_COMMIT \
    ALUXEN_BUILD_COMMIT_SHORT=$ALUXEN_BUILD_COMMIT_SHORT \
    ALUXEN_BUILD_REF=$ALUXEN_BUILD_REF \
    ALUXEN_BUILD_TIME=$ALUXEN_BUILD_TIME \
    ALUXEN_BUILD_IMAGE=$ALUXEN_BUILD_IMAGE \
    ALUXEN_BUILD_RUN_URL=$ALUXEN_BUILD_RUN_URL \
    ALUXEN_BUILD_NUMBER=$ALUXEN_BUILD_NUMBER

ENTRYPOINT ["/aluxen"]
