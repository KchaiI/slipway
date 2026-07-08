# sample-app

minato の規約 (`Dockerfile` を持ち、`$PORT` で listen する) に従った最小の Web アプリ。

```console
$ minato apps create sample
$ cp -r examples/sample-app /tmp/sample && cd /tmp/sample
$ git init -b main && git add -A && git commit -m "init"
$ git remote add minato http://minato.localtest.me/git/sample.git
$ git push minato main
```
