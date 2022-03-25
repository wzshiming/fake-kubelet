#!/usr/bin/env bash

image=fake-kubelet:test
docker build -t "${image}" .

if [[ ! -d fake-k8s ]]; then
  git clone https://github.com/wzshiming/fake-k8s
fi

cd fake-k8s && IMAGE_FAKE_KUBELET="${image}" ./fake-k8s.test.sh
