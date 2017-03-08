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

@test "Check volume management" {
    #skip 
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume create -d nfs --name t1volume
    [ "$status" -eq 0 ]
    [[ "$output" == "t1volume" ]]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume ls -q
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
	[[ $(echo ${lines[0]} | cut -d'/' -f2) == "t1volume" ]]
	hostPlusVolumeNamet1=${lines[0]} 
	for v in "${lines[@]}"
    do	
	 [[ $v == *"t1volume" ]]
     run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume inspect $v
     [ "$status" -eq 0 ]
    done

	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume inspect $hostPlusVolumeNamet1
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]

	
	# check that members of same tenant can assess volume
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG3 volume inspect ${hostPlusVolumeNamet1}
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG3 volume ls -q
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
	[[ ${lines[0]}==${hostPlusVolumeNamet1} ]]

	# check isolation
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume create -d nfs --name t2volume
    [ "$status" -eq 0 ]
    [[ "$output" == "t2volume" ]]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume ls -q
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
	[[ $(echo ${lines[0]} | cut -d'/' -f2) == "t2volume" ]]
	hostPlusVolumeNamet2=${lines[0]}
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume inspect ${hostPlusVolumeNamet2}
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]

	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume inspect ${hostPlusVolumeNamet2}
    [ "$status" -ne 0 ]
    [[ "$output" == *"Error"* ]]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume ls -q
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
	#[ "${#lines[@]}" = 1 ]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume rm ${hostPlusVolumeNamet2}
    [ "$status" -ne 0 ]
    [[ "$output" == *"Error"* ]]

	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume inspect ${hostPlusVolumeNamet1}
    [ "$status" -ne 0 ]
    [[ "$output" == *"Error"* ]]
	
	# allowing same name
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume create -d nfs --name t1volume
    [ "$status" -eq 0 ]
    [[ "$output" == "t1volume" ]]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume ls -q
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]
	#[ "${#lines[@]}" = 2 ]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume inspect ${hostPlusVolumeNamet1}
    [ "$status" -eq 0 ]
    [[ "$output" != *"Error"* ]]

	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume rm t1volume
    [ "$status" -eq 0 ]
    [[ "$output" == "t1volume" ]]	
	
	# without name
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume create -d nfs
    [ "$status" -ne 0 ]
	#[ "$status" -eq 0 ]
	#newvolume=$output
	#run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume ls -q
    #[ "$status" -eq 0 ]
    #[[ "$output" != *"Error"* ]]

	#run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume rm $newvolume
    #[ "$status" -eq 0 ]
    #[[ "$output" == "$newvolume" ]]

	# remove volumes
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume rm t1volume
    [ "$status" -eq 0 ]
    [[ "$output" == "t1volume" ]]	
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume rm t2volume
    [ "$status" -eq 0 ]
    [[ "$output" == "t2volume" ]]
	
	run checkInvariant
    [ $status = 0 ]
}

@test "Check volume binding without volume create" {
	#skip
	# implicit volume without volume create 
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run --rm --volume-driver nfs -v implicit_myvolume:/data busybox sh -c "echo tenant1 hello  > /data/file.txt"
    [ "$status" -eq 0 ]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run --rm --volume-driver nfs -v implicit_myvolume:/data busybox sh -c "cat /data/file.txt"
	[ "$status" -eq 0 ]
	[[ "$output" == "tenant1 hello" ]]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run --rm --volume-driver nfs -v implicit_myvolume:/data busybox sh -c "echo tenant2 hello  > /data/file.txt"
    [ "$status" -eq 0 ]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run --rm --volume-driver nfs -v implicit_myvolume:/data busybox sh -c "cat /data/file.txt"
	[ "$status" -eq 0 ]
	[[ "$output" == "tenant2 hello" ]]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run --rm --volume-driver nfs -v implicit_myvolume:/data busybox sh -c "cat /data/file.txt"
	[ "$status" -eq 0 ]
	[[ "$output" == "tenant1 hello" ]]
}


@test "Check volume binding with volume create" {
    #skip 
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume create -d nfs --name myvolume
    [ "$status" -eq 0 ]
    [[ "$output" == "myvolume" ]]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run --rm -v myvolume:/data busybox sh -c "echo tenant1 hello  > /data/file.txt"
    [ "$status" -eq 0 ]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run --rm -v myvolume:/data busybox sh -c "cat /data/file.txt"
	[ "$status" -eq 0 ]
	[[ "$output" == "tenant1 hello" ]]
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume create -d nfs --name myvolume
    [ "$status" -eq 0 ]
    [[ "$output" == "myvolume" ]]
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run --rm -v myvolume:/data busybox sh -c "echo tenant2 hello  > /data/file.txt"
    [ "$status" -eq 0 ]

	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run --rm -v myvolume:/data busybox sh -c "cat /data/file.txt"
	[ "$status" -eq 0 ]
	[[ "$output" == "tenant1 hello" ]]

	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 run --rm -v myvolume:/data busybox sh -c "cat /data/file.txt"
	[ "$status" -eq 0 ]
	[[ "$output" == "tenant2 hello" ]]	

	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 volume rm myvolume
	[ "$status" -eq 0 ]
	[[ "$output" == "myvolume" ]]	
	
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG2 volume rm myvolume
	[ "$status" -eq 0 ]
	[[ "$output" == "myvolume" ]]
	
	run checkInvariant
    [ $status = 0 ]

}	



@test "Check volume mount to host file system not permitted" {
    skip	
	# error case trying volume mount to host file system not permitted
	run docker -H $SWARM_HOST --config $DOCKER_CONFIG1 run -v /tmp:/data busybox sh -c "echo tenant1 hello  > /data/file.txt"
    [ "$status" -ne 0 ]
	[[ "$output" == *"Error"* ]]
	
	run checkInvariant
    [ $status = 0 ]
}
	









