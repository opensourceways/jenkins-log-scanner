FROM swr.cn-north-4.myhuaweicloud.com/openeuler/go:1.23.4-oe2403lts as BUILDER
    
ENV GO_VERSION=1.23.4
ENV PATH="/usr/local/go/bin:${PATH}"

RUN dnf update -y && \
    dnf install -y git wget \
    go env -w GOPROXY=https://goproxy.cn,direct

ARG USER
ARG PASS
RUN echo "machine github.com login $USER password $PASS" > /root/.netrc
RUN go env -w GOPRIVATE=github.com/opensourceways

# build binary
RUN cd /go/src/github.com/opensourceways/jenkins-log-scanner && GO111MODULE=on CGO_ENABLED=0 go build -buildmode=pie --ldflags "-s -extldflags '-Wl,-z,now'"
RUN curl -sL "https://gitee.com/opensourceway/sec_efficiency_tool/releases/download/1.0.0/gitleaks_8.27.0_linux_x64.tar.gz" -o gitleaks.tar.gz
RUN tar -xzf gitleaks.tar.gz gitleaks
RUN wget https://obs-community.obs.cn-north-1.myhuaweicloud.com/obsutil/current/obsutil_linux_amd64.tar.gz
RUN tar -xzf obsutil_linux_amd64.tar.gz obsutil
COPY . /go/src/github.com/opensourceways/jenkins-log-scanner
# copy binary config and utils
FROM openeuler/openeuler:24.03-lts
RUN dnf -y update && \
    dnf in -y shadow && \
    groupadd -g 1000 jenkins-log-scanner && \
    useradd -u 1000 -g jenkins-log-scanner -s /bin/bash -m jenkins-log-scanner && \
    dnf remove all

USER jenkins-log-scanner
WORKDIR /opt/app/

COPY  --chown=jenkins-log-scanner --from=BUILDER /go/src/github.com/opensourceways/jenkins-log-scanner/jenkins-log-scanner /opt/app

RUN chmod 550 /opt/app/jenkins-log-scanner

ENTRYPOINT ["/opt/app/jenkins-log-scanner"]