FROM ubuntu:24.04

LABEL MAINTAINER="Lucky-Mano"

ENV USER=ubuntu
ENV WORKSPACE=/home/${USER}/workspace

RUN apt-get update && apt-get install -y --no-install-recommends \
  bubblewrap \
  build-essential \
  ca-certificates \
  curl \
  git \
  gnupg \
  jq \
  openssh-client \
  pkg-config \
  procps \
  psmisc \
  ripgrep \
  socat \
  sudo \
  util-linux \
  wget \
  && echo "${USER} ALL=(root) NOPASSWD:ALL" > /etc/sudoers.d/${USER} \
  && chmod 0440 /etc/sudoers.d/${USER} \
  && rm -rf /var/lib/apt/lists/*

USER ${USER}

ENV PATH=$PATH:/home/${USER}/.local/bin:/home/${USER}/.local/share/mise/shims

RUN echo 'eval "$(/home/${USER}/.local/bin/mise activate bash)"' >> /home/${USER}/.bashrc \
  && echo 'export UV_LINK_MODE=copy' >> /home/${USER}/.bashrc \
  && curl https://mise.run | bash

WORKDIR ${WORKSPACE}

CMD ["sleep", "infinity"]
