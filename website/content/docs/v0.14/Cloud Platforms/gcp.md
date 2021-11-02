---
title: "GCP"
description: "Creating a cluster via the CLI on Google Cloud Platform."
---

## Creating a Cluster via the CLI

In this guide, we will create an HA Kubernetes cluster in GCP with 1 worker node.
We will assume an existing [Cloud Storage bucket](https://cloud.google.com/storage/docs/creating-buckets), and some familiarity with Google Cloud.
If you need more information on Google Cloud specifics, please see the [official Google documentation](https://cloud.google.com/docs/).

[jq](https://stedolan.github.io/jq/) and [talosctl](../../introduction/quickstart/#talosctl) also needs to be installed

### Environment Setup

We'll make use of the following environment variables throughout the setup.
Edit the variables below with your correct information.

```bash
# Storage account to use
export STORAGE_BUCKET="StorageBucketName"
# Region
export REGION="us-central1"
```

### Create the Image

First, download the Google Cloud image from a Talos [release](https://github.com/talos-systems/talos/releases).
These images are called `gcp-$ARCH.tar.gz`.

#### Upload the Image

Once you have downloaded the image, you can upload it to your storage bucket with:

```bash
gsutil cp /path/to/gcp-amd64.tar.gz gs://$STORAGE_BUCKET
```

#### Register the image

Now that the image is present in our bucket, we'll register it.

```bash
gcloud compute images create talos \
 --source-uri=gs://$STORAGE_BUCKET/gcp-amd64.tar.gz \
 --guest-os-features=VIRTIO_SCSI_MULTIQUEUE
```

### Network Infrastructure

#### Load Balancers and Firewalls

##### Manual Setup

Once the image is prepared, we'll want to work through setting up the network.
Issue the following to create a firewall, load balancer, and their required components.

`130.211.0.0/22` and `35.191.0.0/16` are the GCP [Load Balancer IP ranges](https://cloud.google.com/load-balancing/docs/health-checks#fw-rule)

```bash
# Create Instance Group
gcloud compute instance-groups unmanaged create talos-ig \
  --zone $REGION-b

# Create port for IG
gcloud compute instance-groups set-named-ports talos-ig \
    --named-ports tcp6443:6443 \
    --zone $REGION-b

# Create health check
gcloud compute health-checks create tcp talos-health-check --port 6443

# Create backend
gcloud compute backend-services create talos-be \
    --global \
    --protocol TCP \
    --health-checks talos-health-check \
    --timeout 5m \
    --port-name tcp6443

# Add instance group to backend
gcloud compute backend-services add-backend talos-be \
    --global \
    --instance-group talos-ig \
    --instance-group-zone $REGION-b

# Create tcp proxy
gcloud compute target-tcp-proxies create talos-tcp-proxy \
    --backend-service talos-be \
    --proxy-header NONE

# Create LB IP
gcloud compute addresses create talos-lb-ip --global

# Forward 443 from LB IP to tcp proxy
gcloud compute forwarding-rules create talos-fwd-rule \
    --global \
    --ports 443 \
    --address talos-lb-ip \
    --target-tcp-proxy talos-tcp-proxy

# Create firewall rule for health checks
gcloud compute firewall-rules create talos-controlplane-firewall \
     --source-ranges 130.211.0.0/22,35.191.0.0/16 \
     --target-tags talos-controlplane \
     --allow tcp:6443

# Create firewall rule to allow talosctl access
gcloud compute firewall-rules create talos-controlplane-talosctl \
  --source-ranges 0.0.0.0/0 \
  --target-tags talos-controlplane \
  --allow tcp:50000
```

##### Using GCP Deployment Manager

```yaml
resources:
- type: compute.v1.instanceGroup
  name: talos-ig
  properties:
    description: Talos instance group
    namedPorts:
    - name: tcp6443
      port: 6443
- type: compute.v1.healthCheck
  name: talos-healthcheck
  properties:
    description: Talos health check
    type: TCP
    tcpHealthCheck:
      port: 6443
- type: compute.v1.backendService
  name: talos-be
  properties:
    description: Talos backend service
    protocol: TCP
    healthChecks:
    - $(ref.talos-healthcheck.selfLink)
    timeoutSec: 300
    backends:
    - description: Talos backend
      group: $(ref.talos-ig.selfLink)
    portName: tcp6443
- type: compute.v1.targetTcpProxy
  name: talos-tcp-proxy
  properties:
    description: Talos TCP proxy
    service: $(ref.talos-be.selfLink)
    proxyHeader: NONE
- type: compute.v1.globalAddress
  name: talos-lb-ip
  properties:
    description: Talos LoadBalancer IP
- type: compute.v1.globalForwardingRule
  name: talos-fwd-rule
  properties:
    description: Talos Forwarding rule
    target: $(ref.talos-tcp-proxy.selfLink)
    IPAddress: $(ref.talos-lb-ip.address)
    IPProtocol: TCP
    portRange: 443
- type: compute.v1.firewall
  name: talos-controlplane-firewall
  properties:
    description: Talos controlplane firewall
    sourceRanges:
    - 130.211.0.0/22
    - 35.191.0.0/16
    targetTags:
    - talos-controlplane
    allowed:
    - IPProtocol: TCP
      ports:
      - 6443
- type: compute.v1.firewall
  name: talos-controlplane-talosctl
  properties:
    description: Talos controlplane talosctl firewall
    sourceRanges:
    - 0.0.0.0/0
    targetTags:
    - talos-controlplane
    allowed:
    - IPProtocol: TCP
      ports:
      - 50000
outputs:
- name: loadbalancerIP
  value: $(ref.talos-lb-ip.address)
```

Save the above as `network-infrastructure.yaml` and run the following command to bring up the infra

```bash
gcloud deployment-manager deployments create \
  network-infrastructure \
  --config network-infrastructure.yaml
```

### Cluster Configuration

With our networking bits setup, we'll fetch the IP for our load balancer and create our configuration files.

```bash
LB_PUBLIC_IP=$(gcloud compute forwarding-rules describe talos-fwd-rule \
               --global \
               --format json \
               | jq -r .IPAddress)

talosctl gen config talos-k8s-gcp-tutorial https://${LB_PUBLIC_IP}:443
```

Additionally, you can specify `--config-patch` with RFC6902 jsonpatch which will be applied during the config generation.

### Compute Creation

We are now ready to create our GCP nodes.

```bash
# Create the control plane nodes.
for i in $( seq 1 3 ); do
  gcloud compute instances create talos-controlplane-$i \
    --image talos \
    --zone $REGION-b \
    --tags talos-controlplane \
    --boot-disk-size 20GB \
    --metadata-from-file=user-data=./controlplane.yaml
done

# Add control plane nodes to instance group
for i in $( seq 0 1 3 ); do
  gcloud compute instance-groups unmanaged add-instances talos-ig \
      --zone $REGION-b \
      --instances talos-controlplane-$i
done

# Create worker
gcloud compute instances create talos-worker-0 \
  --image talos \
  --zone $REGION-b \
  --boot-disk-size 20GB \
  --metadata-from-file=user-data=./worker.yaml
```

### Bootstrap Etcd

You should now be able to interact with your cluster with `talosctl`.
We will need to discover the public IP for our first control plane node first.

```bash
CONTROL_PLANE_0_IP=$(gcloud compute instances describe talos-controlplane-0 \
                     --zone $REGION-b \
                     --format json \
                     | jq -r '.networkInterfaces[0].accessConfigs[0].natIP')
```

Set the `endpoints` and `nodes`:

```bash
talosctl --talosconfig talosconfig config endpoint $CONTROL_PLANE_0_IP
talosctl --talosconfig talosconfig config node $CONTROL_PLANE_0_IP
```

Bootstrap `etcd`:

```bash
talosctl --talosconfig talosconfig bootstrap
```

### Retrieve the `kubeconfig`

At this point we can retrieve the admin `kubeconfig` by running:

```bash
talosctl --talosconfig talosconfig kubeconfig .
```

### Cleanup

#### Manual

```bash
# cleanup VM's
gcloud compute instances delete \
  talos-worker-0 \
  talos-controlplane-0 \
  talos-controlplane-1 \
  talos-controlplane-2

# cleanup firewall rules
gcloud compute firewall-rules delete \
  talos-controlplane-talosctl \
  talos-controlplane-firewall

# cleanup forwarding rules
gcloud compute forwarding-rules delete \
  talos-fwd-rule

# cleanup addresses
gcloud compute addresses delete \
  talos-lb-ip

# cleanup proxies
gcloud compute target-tcp-proxies delete \
  talos-tcp-proxy

# cleanup backend services
gcloud compute backend-services delete \
  talos-be

# cleanup health checks
gcloud compute health-checks delete \
  talos-health-check

# cleanup unmanaged instance groups
gcloud compute instance-groups unmanaged delete \
  talos-ig

# cleanup Talos image
gcloud compute images delete \
  talos
```

#### Using Deployment Manager

```bash
# cleanup VM's
gcloud compute instances delete \
  talos-worker-0 \
  talos-controlplane-0 \
  talos-controlplane-1 \
  talos-controlplane-2

# cleanup infrastructure deployment
gcloud deployment-manager deployments delete \
  network-infrastructure

# cleanup Talos image
gcloud compute images delete \
  talos
```
