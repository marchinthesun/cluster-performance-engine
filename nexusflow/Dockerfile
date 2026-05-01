# syntax=docker/dockerfile:1.7
# Single image: bookworm + kubectl + nexusflow (supervisor = optional kube deploy + Plasma + dashboard).
# Context: this directory (Go module root).
#
#   docker build -t nexusflow:local .
#   docker run --rm -p 9842:9842 nexusflow:local
#
# With cluster (host network on Linux often needed for Minikube):
#   docker run --rm --network host -v ~/.kube/config:/kube/config:ro nexusflow:local
#
# Skip manifest apply (Plasma demo only):  -e SKIP_KUBE_DEPLOY=1

ARG GO_VERSION=1.22

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o /out/nexusflow ./cmd/nexusflow

FROM debian:bookworm-slim
ARG KUBECTL_VERSION=v1.30.3
RUN apt-get update \
  && apt-get install -y --no-install-recommends python3 python3-pip ca-certificates util-linux hwloc curl \
  && rm -rf /var/lib/apt/lists/* \
  && set -eux \
  && ARCH="$(dpkg --print-architecture)" \
  && case "$ARCH" in amd64) KARCH=amd64 ;; arm64) KARCH=arm64 ;; *) echo "unsupported arch $ARCH" >&2; exit 1 ;; esac \
  && curl -fsSL -o /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${KARCH}/kubectl" \
  && chmod +x /usr/local/bin/kubectl \
  && kubectl version --client=true --output=yaml >/dev/null

COPY --from=build /out/nexusflow /usr/local/bin/nexusflow
RUN ln -sf /usr/local/bin/nexusflow /usr/local/bin/kubermetrics
COPY sdk/python /opt/nexusflow-sdk
RUN pip3 install --break-system-packages -e /opt/nexusflow-sdk \
  || pip3 install -e /opt/nexusflow-sdk

COPY examples/plasma /demo
COPY examples/pipeline.yaml /demo/ref/pipeline.yaml
COPY examples/ci/build-ci-sample.yaml /demo/ref/build-ci-sample.yaml
COPY examples/ml/etl-numa-dag.yaml /demo/ref/etl-numa-dag.yaml
RUN sed -i 's/\r$//' /demo/docker-entry.sh /demo/worker_branch.py /demo/worker_leaf.py \
  && chmod +x /demo/docker-entry.sh /demo/worker_branch.py /demo/worker_leaf.py

WORKDIR /demo
ENV NEXUSFLOW_PLASMA_SOCK=/run/plasma.sock \
    KUBECONFIG=/kube/config

CMD ["/usr/local/bin/nexusflow"]
