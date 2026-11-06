#!/usr/bin/env python3
"""Validate a ko build digest, or resolve the public main image for manual runs."""
import hashlib
import json
import re
import sys
import urllib.request

REPOSITORY = 'modelcontextprotocol/registry'
IMAGE = 'ghcr.io/' + REPOSITORY


def validate(image):
    if not re.fullmatch(re.escape(IMAGE) + r'@sha256:[0-9a-f]{64}', image):
        raise ValueError('Expected the registry build digest')
    return image


def resolve():
    url = 'https://ghcr.io/token?service=ghcr.io&scope=repository:' + REPOSITORY + ':pull'
    with urllib.request.urlopen(url, timeout=30) as response:
        token = json.load(response)['token']
    request = urllib.request.Request('https://ghcr.io/v2/' + REPOSITORY + '/manifests/main', headers={
        'Authorization': 'Bearer ' + token,
        'Accept': ', '.join(['application/vnd.oci.image.index.v1+json',
                            'application/vnd.docker.distribution.manifest.list.v2+json',
                            'application/vnd.oci.image.manifest.v1+json',
                            'application/vnd.docker.distribution.manifest.v2+json']),
    })
    with urllib.request.urlopen(request, timeout=30) as response:
        body = response.read()
        digest = response.headers['Docker-Content-Digest']
    if digest != 'sha256:' + hashlib.sha256(body).hexdigest():
        raise ValueError('Manifest digest mismatch')
    return validate(IMAGE + '@' + digest)


if __name__ == '__main__':
    try:
        if len(sys.argv) > 2:
            raise ValueError('Expected zero or one argument')
        print(validate(sys.argv[1]) if len(sys.argv) == 2 else resolve())
    except (ValueError, KeyError, OSError):
        sys.exit('Could not resolve a verified staging image digest')
