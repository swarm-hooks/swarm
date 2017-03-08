#!/usr/bin/env bats

######################################################################################
# cli.bats tests multi-tenant swarm
# The following environment variables are required
# SWARM_HOST The IP and Port of the SWARM HOST.  It is in form of tcp://<ip>:<port>
# DOCKER_CONFIG1  Directory where the docker config.json file for the Tenant 1, User 1
# DOCKER_CONFIG2  Directory where the docker config.json file for the Tenant 2, User 2
# DOCKER_CONFIG3  Directory where the docker config.json file for the Tenant 1, User 3
#
# Notes on test logic
#  Before each test all containers are remove from the Tenant 1 and Tenant 2 (see setup()))
#  After each test the invariant is checked (checkInvariant()).  checkInvariant checks
#  Tenant 1 and Tenant 2 containers are different from each other and that User 1 and User 3
#  containers are the same.
######################################################################################
  

load cli_helpers

@test "Check overlay" {
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 network rm nw
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 network create -d overlay nw
	[ "$status" -eq 0 ]
    t1_nw_id=$output
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 network inspect nw
	[ "$status" -eq 0 ]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 network inspect $t1_nw_id
	[ "$status" -eq 0 ]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run --net=nw -itd -p 80 --name=cont1 busybox httpd -f -p 80
	[ "$status" -eq 0 ]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run --net=nw -itd -p 80 --name=cont2 busybox httpd -f -p 80
	[ "$status" -eq 0 ]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 exec cont1 ping -c 2 cont2
	[ "$status" -eq 0 ]
	[[ "$output" == *"2 packets transmitted, 2 packets received, 0% packet loss"* ]]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 exec cont2 ping -c 2 cont1
	[ "$status" -eq 0 ]
	[[ "$output" == *"2 packets transmitted, 2 packets received, 0% packet loss"* ]]
	run  docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run --net=nw --rm -p 80  radial/busyboxplus:curl curl -s http://cont1:80 
	[ "$status" -eq 0 ]
	[[ "$output" == *"<HTML><HEAD><TITLE>"* ]]
	run  docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run --net=nw --rm -p 80  radial/busyboxplus:curl curl -s http://cont1:80 
	[ "$status" -ne 0 ]
	[[ "$output" != *"<HTML><HEAD><TITLE>"* ]]

}
