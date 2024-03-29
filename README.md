# flashblade-plumbing

Program to validate FlashBlade connectivity and performance from a client. Answers the question "What throughput does the data network between my machine and the FlashBlade support?"

This program is intended to validate NFSv3 and S3 read/write performance from a single client to a FlashBlade with minimal dependencies. The result is a single Go program with minimal input required: 1) the FlashBlade management VIP and 2) login token. Specify these using environment variables FB_MGMT_VIP and FB_TOKEN. It is also possible to manually specify the data VIP and filesystem or bucket names to test against.

The token can be created or retrieved via the FlashBlade CLI:

```pureadmin [create|list] --api-token --expose```

This program operates by using the FlashBlade REST API to create test filesystems and object store accounts/key/buckets, then uses userspace NFS v3 (port 2049) and S3 code (port 80) to test write and read performance, and finally cleans up all filesystems and accounts. The IO pattern is multiple threads doing sequential writes and reads to large files/objects with random, uncompressible data. In case of multiple data VIPs defined on the FlashBlade, this tool will test against one data VIP per configured subnet. If a connection fails or a mount times out, the program will proceed to the next data VIP and continue testing.

The automatic provisioning mode requires clients to be able to access the FlashBlade management VIP and will not work with a read-only API token or if SafeMode is enabled. In these cases, use the manual provision mode described below.

An example output looks like below, where the client can only reach the FlashBlade on one of the configured data VIPs:
```
dataVip,protocol,result,write_tput,read_tput
192.168.170.11,nfs,SUCCESS,3.1 GB/s,4.0 GB/s
192.168.40.11,nfs,MOUNT FAILED,-,-
192.168.40.11,s3,FAILED TO CONNECT,-,-
192.168.170.11,s3,SUCCESS,1.7 GB/s,4.3 GB/s
```

Since the token is required to have full permissions, it is recommended to delete and recreate the token after testing completed and before moving to production (in case it was leaked during the test setup). The token can be deleted by 
```pureadmin delete --api-token username```

## Running

The tool has two major modes of operation: autoprovisioning and manual provisioning.

### Automatic Provisioning (default)

Relies on the FB_MGMT_VIP and FB_TOKEN environment variables to log in to the FlashBlade and find/create all the necessary entities (datavips, filesystems, access keys, etc) for the test and then clean up afterwards.

### Manual Provisioning

If either command-line option "--bucket" or "--filesystem" is specified, the tool falls back to manual mode where it assumes the filesystem and/or bucket already exist. As a result, it no longer needs to connect to the FlashBlade REST API. This means that the tool can be run against non-FlashBlade endpoints. The filesystem is required to support NFS v3 (v4 not supported in the tool at this time).

In this mode, the "--datavip" argument is required and the FB_MGMT_VIP/FB_TOKEN are no longer required. Credentials for the S3 access should be configured using [standard AWS SDK](https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/configuring-sdk.html#specifying-credentials) environment variables or the /.aws/credentials file.

### Kubernetes

The tool can be run within Kubernetes via a Daemonset or a simple Job.  See the example [daemonset](k8s-daemonset.yaml) and [job](k8s-runner.yaml) and insert your MGMT_VIP and TOKEN.

The job example includes a nodeSelector to test on a specific kubernetes node.

### Docker

The following docker run invocates the plumbing tool. Use your values for the MGMT_VIP and TOKEN environment variables.

```docker run -it --rm -e FB_MGMT_VIP=$FB_MGMT_VIP -e FB_TOKEN=$FB_MGMT_TOKEN joshuarobinson/go-plumbing:0.4```

Or for manual provisioning, specify existing filesystem and bucket name, along with a shared credentials file.

```
docker run -it --rm -v /home/ir/.aws/credentials:/root/.aws/credentials \
    joshuarobinson/go-plumbing:0.4 \
     --duration 30 --bucket existingbucket --filesystem existingfiles --datavip 10.62.64.200
```

### Binary

Download the Linux binary from the [release page](https://github.com/joshuarobinson/flashblade-plumbing/releases/tag/v0.4).

```
wget https://github.com/joshuarobinson/flashblade-plumbing/releases/download/v0.4/fb-plumbing-v0.4
chmod a+x fb-plumbing-v0.4
FB_MGMT_VIP=REPLACEME FB_TOKEN=REPLACEME ./fb-plumbing-v0.4
```

### Multiple Hosts with Ansible

The following Ansible ad hoc commands first copy the downloaded binary to all nodes and then runs the tool one host at a time using the “--forks” option to control parallelism.

```
ansible myhosts -o -m copy -a "src=fb-plumbing-v0.4 dest=fb-plumbing mode=+x"
ansible myhosts --forks 2 -m shell -a "FB_TOKEN=REPLACEME FB_MGMT_VIP=10.2.6.20 ./fb-plumbing"
```

## Command Line Options

- --skip-nfs, --skip-s3: Skip running either of the protocols as part of the test suite.
- --duration: length of each individual test run (read or write, nfs or s3), in seconds. Default is 60.
- --datavip: allows manually specifying the endpoint to connect to for NFS and S3 tests. By default, the tool queries the FlashBlade and uses one data VIP per subnet.
- --filesystem: specify name of an external filesystem to mount for testing purposes. Must support NFSv3.
- --bucket: specify name of an external bucket to use for testing purposes. Credentials should be provideded via environment variables or credentials file.
