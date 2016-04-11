sudo -E "./_output/local/bin/linux/amd64/kube-vmturbo" \
	 --v=4 \
	 --master="http://127.0.0.1:8080" \
	 --etcd-servers="http://127.0.0.1:4001" \
	 --config-path="./vmt_testing/vmt_testing_config.json" > "/tmp/kube-vmturbo.log" 2>&1 &
