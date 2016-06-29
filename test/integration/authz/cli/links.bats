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
NOTAUTHORIZED="Error response from daemon: No such container or the user is not authorized for this container:"
@test "Check links" {
    #skip "work in progress"
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run -d  --name busy1 busybox top
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
    cid_busy1=$output
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run -d --link busy1  --name busy2 busybox top
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
    cid_busy2=$output
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run -d --link busy1 --link $cid_busy2 --name busy3 busybox top
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
    cid_busy3=$output
	
 	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run -d --link busy1:busy1alias --link $cid_busy2:busy2alias --name busy4 busybox top
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
    cid_busy4=$output
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run -d --link busy1 --link $cid_busy2 --name busy_3 busybox top
    [ "$status" -ne 0 ]
    [[ "$output" == *"Error"* ]]
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run -d --link busy1:busy1alias --link $cid_busy2:busy2alias --name busy_3 busybox top
    [ "$status" -ne 0 ]
    [[ "$output" == *"Error"* ]]
    cid_busy3=$output
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run -d  --name busy1 busybox top
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
    cid_busy1=$output
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run -d --link busy1  --name busy2 busybox top
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
    cid_busy2=$output
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run -d --link busy1 --link $cid_busy2 --name busy3 busybox top
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
    cid_busy3=$output
	
 	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run -d --link busy1:busy1alias --link $cid_busy2:busy2alias --name busy4 busybox top
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
    cid_busy4=$output

 
    run checkInvariant
    [ $status = 0 ]  
}

