FROM openeuler/go:1.23.4-oe2403lts as BUILDER
    
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
COPY . /go/src/github.com/opensourceways/jenkins-log-scanner
# copy binary config and utils
FROM openeuler/openeuler:24.03-lts
RUN dnf -y update && \
    dnf in -y shadow && \
    groupadd -g 1000 jenkins-log-scanner && \
    useradd -u 1000 -g jenkins-log-scanner -s /bin/bash -m jenkins-log-scanner && \
    dnf remove all

RUN echo "umask 027" >> /home/jenkins-log-scanner/.bashrc \
    && echo "umask 027" >> /root/.bashrc \
    && source /home/om-webserver/.bashrc \
    && echo "set +o history" >> /etc/bashrc \
    && echo "set +o history" >> /home/jenkins-log-scanner/.bashrc \
    && sed -i "s|HISTSIZE=1000|HISTSIZE=0|" /etc/profile \
    && sed -i "s|PASS_MAX_DAYS[ \t]*99999|PASS_MAX_DAYS 30|" /etc/login.defs \
    && sed -i '4,6d' /home/om-webserver/.bashrc \

USER jenkins-log-scanner
WORKDIR /opt/app/

COPY  --chown=jenkins-log-scanner --from=BUILDER /go/src/github.com/opensourceways/jenkins-log-scanner/jenkins-log-scanner /opt/app

RUN chmod 550 /opt/app/jenkins-log-scanner

ENTRYPOINT ["/opt/app/jenkins-log-scanner"]