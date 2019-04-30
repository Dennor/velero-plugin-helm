# Velero Helm Plugin

This repository contains velero plugin which can backup helm releases deployed by tiller.

## Using the plugin

To use the plugin just add it to velero.

```
$ velero plugin add dennor/velero-plugin-helm:latest
```

## Example of backup and restore

1. Deploy example chart `nging-chart`

```
$ helm install --name nginx-example-release ./examples/nginx-chart
```

2. Once it's the release is deployed and ready create a backup

```
$ velero backup create nginx-example-release-backup -l "OWNER=TILLER,NAME=nginx-example-release"
$ velero backup describe nginx-example-release-backup
```

3. "Accidentally" delete the release.

```
$ helm delete --purge nginx-example-release
```

4. Restore release

```
$ velero restore create --from-backup nginx-example-release-backup
```

## Build image

```
$ make clean
$ make container IMAGE=dennor/velero-plugin-helm:latest
```

## Limitations

* Cluster scoped resources won't be backed up - if anyone can tell me how to tell velero that `ResourceIdentifier` is cluster scoped I will implement it.
