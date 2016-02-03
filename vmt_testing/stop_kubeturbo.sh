TURBO_PID=`/bin/ps -fu $USER| grep "kube-vmturbo " | grep -v "grep" | awk '{print $2}'`
echo $TURBO_PID
kill $TURBO_PID
