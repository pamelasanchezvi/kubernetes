sudo -E  "./_output/local/bin/linux/amd64/kube-vmtactionsimulator" \
	 --v=3 \
	 --master="http://127.0.0.1:8080" \
	 --etcd-servers="http://127.0.0.1:4001" \
	 --pod="redis-slave-2kup0" \
	 --action="move" \
	 --destination="127.0.0.1">/tmp/kube-vmturbo.log 2>&1