# flashblade-plumbing

Program to validate FlashBlade connectivity and performance from a client. Answers the question "What throughput does the data network between my machine and the FlashBlade support?"

This program is intended to validate NFS and S3 read/write performance from a single client to a FlashBlade with minimal dependencies. The result is a single Go program with minimal input required: 1) the FlashBlade management VIP and 2) login token. Specify these using environment variables FB_MGMT_VIP and FB_TOKEN.

The token can be created or retrieved via the FlashBlade CLI:

```pureadmin [create|list] --api-token --expose```

This program operates by using the FlashBlade REST API to create test filesystems and object store accounts/key/buckets, then uses userspace NFS (port 2049) and S3 code (port 80) to test write and read performance, and finally cleans up all filesystems and accounts. The IO pattern is multiple threads doing sequential writes and reads to large files/objects with random, uncompressible data. In case of multiple data VIPs defined on the FlashBlade, this tool will test against one data VIP per configured subnet. If a connection fails or a mount times out, the program will proceed to the next data VIP and continue testing.

This tool requires clients to be able to access the FlashBlade management VIP and will not work with a read-only API token or if SafeMode is enabled.

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

### Kubernetes

The tool can be run within Kubernetes via a Daemonset or a simple Job.  See the example [daemonset](k8s-daemonset.yaml) and [job](k8s-runner.yaml) and insert your MGMT_VIP and TOKEN.

The job example includes a nodeSelector to test on a specific kubernetes node.

### Docker

The following docker run invocates the plumbing tool. Use your values for the MGMT_VIP and TOKEN environment variables.

```docker run -it --rm -e FB_MGMT_VIP=$FB_MGMT_VIP -e FB_TOKEN=$FB_MGMT_TOKEN joshuarobinson/go-plumbing:0.3```

### Binary

Download the Linux binary from the [release page](https://github.com/joshuarobinson/flashblade-plumbing/releases/tag/v0.3).

```
wget https://github.com/joshuarobinson/flashblade-plumbing/releases/download/v0.3/fb-plumbing-v0.3
chmod a+x fb-plumbing-v0.3
FB_MGMT_VIP=REPLACEME FB_TOKEN=REPLACEME ./fb-plumbing-v0.3
```

### Multiple Hosts with Ansible

The following Ansible ad hoc commands first copy the downloaded binary to all nodes and then runs the tool one host at a time using the “--forks” option to control parallelism.

```
ansible myhosts -o -m copy -a "src=fb-plumbing-v0.3 dest=fb-plumbing mode=+x"
ansible myhosts --forks 2 -m shell -a "FB_TOKEN=REPLACEME FB_MGMT_VIP=10.2.6.20 ./fb-plumbing"
```

## Command Line Options

- -skip-nfs, -skip-s3: Skip running either of the protocols as part of the test suite.
- -test-duration: length of each individual test run (read or write, nfs or s3), in seconds. Default is 60.
- -datavip a.b.c.d: allows manually specifying the endpoint to connect to for NFS and S3 tests. By default, the tool queries the FlashBlade and uses one data VIP per subnet.
