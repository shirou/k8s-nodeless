# k8s-nodeless

`k8s-nodeless` is a tiny tool to run a workload on Serverless runtime(ex: AWS Lambda) under a kubernetes manner.

## How to use

### 0. Create a Serverless function

Before using this tool, you should deploy your Serverless function by using any kind of deploy tools.


### 1. Create k8s manifest

Create kubernetes manifest which use k8s-nodeless Docker image.


```
apiVersion: batch/v1
kind: Job
metadata:
  name: k8s-nodeless-lambda
spec:
  template:
    spec:
      serviceAccountName: lambda-execute
      containers:
      - name: nodeless
        image: shirou/k8s-nodeless
        env:
          - name: FUNC
            value: "arn:aws:lambda:ap-northeast-1:999999999:function:test-eks-lambda-invoke"
          - name: JSON
            value: "true"
          - name: PAYLOAD
            value: "{\"time\": \"fooo\"}"
      restartPolicy: Never
  backoffLimit: 1
```

Note: if you use AWS eks, IAMServiceAccount is need to execute Lambda.

```
eksctl create iamserviceaccount --name lambda-execute --cluster sampleprj-stg-1 \
--attach-policy-arn arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole \
--attach-policy-arn arn:aws:iam::aws:policy/service-role/AWSLambdaRole \
--approve
```

### 2. Run a Job with kurbernetes manner

```
$ kubectl apply -f lambda_job.yaml
```

## Options

All options can be set by using environment variables.

- `-func` or `FUNC`: function name
- `-payload_file` or `PAYLOAD_FILE`: speficy request payload file
- `-payload` or `PAYLOAD`: request payload. higher priority than file
- `-json` or `JSON`: enable JSON log format
- `-vendor` or `VENDOR`: vendor name(currently only "aws") (default "aws")

## License

Apache License

