package raft

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"labrpc"
	"math/rand"
	"sync"
	"time"
)

type ApplyMsg struct {
	Index       int
	Command     interface{}
	UseSnapshot bool
	Snapshot    []byte
}

type Raft struct {
	mu sync.Mutex // this is the mutex to protect access to shared variables

	peers           []*labrpc.ClientEnd //this array stores the  peers in the distibuted systems
	persister       *Persister
	me              int         //this is my index in the peers array
	currentTerm     int         //this is my current term
	votedFor        int         //this is the index of the node that i have voted for
	myRole          string      //this is a string that stores my role (candidate, leader or follower)
	tempTerm        int         //this is used to ensure an atomic transition when the terms are changed
	tempVote        int         //this is used to ensure an atomic transition when the voted for is changed
	signalChannel   chan string //this is used to signal the completion of a change
	updateStatus    chan string //this is used to signal the vchange in node status
	signalElections chan string // this is used to signal the  start of an election

	////part 3
	log          []Log
	lastApplied  int
	commitIndex  int
	nextIndex    []int
	matchIndex   []int
	prevLogIndex int
	applyChannel chan ApplyMsg
}

// this function, when called, checks the role and term of the node
// returns the current term  AND
// return true if it is a leader
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool

	rf.mu.Lock()               //lock to ensure exclusive accessof myrole
	if rf.myRole == "Leader" { //if it is a leader, then return the term and TRUE
		term = rf.currentTerm
		isleader = true

	} else {
		term = rf.currentTerm //if it is not a leader, then return the term and FALSE
		isleader = false

	}
	rf.mu.Unlock() //Unlock the mutex because done operating on the shared variables
	return term, isleader
}

func (rf *Raft) persist() {
	// Your code here.
	// Example:
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)
	//rf.mu.Lock()
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	//rf.mu.Unlock()
	data := w.Bytes()
	rf.persister.SaveRaftState(data)

}

func (rf *Raft) readPersist(data []byte) {
	if data != nil && len(data) >= 1 {

		r := bytes.NewBuffer(data)
		d := gob.NewDecoder(r)
		rf.mu.Lock()
		d.Decode(&rf.currentTerm)
		d.Decode(&rf.votedFor)
		d.Decode(&rf.log)
		rf.mu.Unlock()
	}

}

type RequestVoteArgs struct { //this is the struct that stores the arguments that are to be sent by the candidate to its peers while requesting for votes
	Term         int //this is the term of the candidate
	CandidateId  int // this is the id of the candidate that is requesting vote
	LastLogIndex int //this is the last log index of the candidate
	LastLogTerm  int
}
type RequestVoteReply struct { //this is the struct that stores the response of the node that was requested for votes by a candidate
	Term        int  //this is the term of the node that received the request for vote
	VoteGranted bool //this is if the node that received request to be voted gives the vote to the candidate or not

}

type AppendEntriesArgs struct { //this is the struct that stores the arguments that are to be sent by leader to all peers as a part of log consistency and heartbeats
	Term     int //this is the term of the leader
	LeaderId int //this is the id of the leader

	PrevLogIndex int
	PrevLogTerm  int
	Entries      []Log
	LeaderCommit int
}
type AppendEntriesReply struct { //this is the response of append entries that are sent by leader to all candidates
	Term    int  //this is the term of the follower
	Success bool //this is if the log consistency check was passed or not
}

type Log struct {
	Term    int
	Command interface{}
}

//this function is the handler that is invoked when a node receives a request for votes

func (rf *Raft) RequestVote(args RequestVoteArgs, reply *RequestVoteReply) {
	fmt.Println("server number ", rf.me, "received a request for votes", args)
	rf.mu.Lock()                     //lock to ensure exclusive access to the term of the node
	if args.Term >= rf.currentTerm { //is the candidate's term is greater than or equal to the term of this node

		if rf.currentTerm < args.Term { // if the canadidate/s term is strictly greater than its own term, then it will transition to a follower
			//check if the log is at least as consistent and if it is then u can give vote, otherwise dont
			//if you have a log entry at the lastlog index

			loglength := len(rf.log)

			fmt.Println("server number", rf.me, "my loglength is ", loglength)
			compTerm := -1
			fmt.Println("compterm is", compTerm)
			if args.LastLogIndex < loglength {

				compTerm := rf.log[args.LastLogIndex].Term
				if compTerm > args.LastLogTerm {
					fmt.Println("server number ", rf.me, "not giving vote to ", args, " because my last log term is greater--", compTerm)
					reply.Term = rf.currentTerm
					rf.currentTerm = args.Term
					rf.mu.Unlock()
					reply.VoteGranted = false
					//dont give vote
				} else if compTerm == args.LastLogTerm {
					//check for log length
					if loglength-1 > args.LastLogIndex {
						//i have a greater log length
						//dontn give vote
						fmt.Println("server number ", rf.me, "not giving vote to ", args, " because i have a greater log length and same lag log term")
						reply.Term = rf.currentTerm
						rf.currentTerm = args.Term
						rf.mu.Unlock()
						reply.VoteGranted = false
					} else {
						//give vote here

						fmt.Println("server number ", rf.me, "decided to give vote to ", args, "last log term is same but they have a greater or equal log length")
						rf.mu.Unlock()
						reply.Term = args.Term
						reply.VoteGranted = true
						fmt.Println("server number ", rf.me, " pushing follower to update status channel in request vote")
						rf.updateStatus <- "Follower"
						rf.mu.Lock()
						fmt.Println("server number ", rf.me, " setting temp term to: ", args.Term, " temp vote to : ", args.CandidateId, "in request vote")
						rf.tempTerm = args.Term
						rf.tempVote = args.CandidateId
						rf.mu.Unlock()
						fmt.Println("server number ", rf.me, " pushing signal to signal channel in request vote")
						rf.signalChannel <- "signal"

					}
				} else {
					//give vote
					fmt.Println("server number ", rf.me, "decided to give vote to because their last log term is greater", args)
					rf.mu.Unlock()
					reply.Term = args.Term
					reply.VoteGranted = true
					fmt.Println("server number ", rf.me, " pushing follower to update status channel in request vote")
					rf.updateStatus <- "Follower"
					rf.mu.Lock()
					fmt.Println("server number ", rf.me, " setting temp term to: ", args.Term, " temp vote to : ", args.CandidateId, "in request vote")
					rf.tempTerm = args.Term
					rf.tempVote = args.CandidateId
					rf.mu.Unlock()
					fmt.Println("server number ", rf.me, " pushing signal to signal channel in request vote")
					rf.signalChannel <- "signal"
				}

			} else {
				fmt.Println("server number ", rf.me, "decided to give vote to ", args)
				rf.mu.Unlock()
				reply.Term = args.Term
				reply.VoteGranted = true
				fmt.Println("server number ", rf.me, " pushing follower to update status channel in request vote")
				rf.updateStatus <- "Follower"
				rf.mu.Lock()
				fmt.Println("server number ", rf.me, " setting temp term to: ", args.Term, " temp vote to : ", args.CandidateId, "in request vote")
				rf.tempTerm = args.Term
				rf.tempVote = args.CandidateId
				rf.mu.Unlock()
				fmt.Println("server number ", rf.me, " pushing signal to signal channel in request vote")
				rf.signalChannel <- "signal"

			}

		} else { //if the term is same as the term of the node that is requesting vote then dont give vote
			fmt.Println("server number ", rf.me, "not giving vote to ", args, " because same term hai in request vote")
			reply.Term = rf.currentTerm
			rf.mu.Unlock()
			reply.VoteGranted = false

		}

	} else { //dont give vote if you receive a request for votes from a node living past
		//if it is living in the past but it has a last term that is greater than urs, then give vote
		//	if the last term is equal to yours, then check for last log index
		//		if the last log index is greater for uu, then dont give vote
		//		if the last log index is lower then give vote
		fmt.Println("server number ", rf.me, "not giving vote to ", args, " becuase old term hai")
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		rf.mu.Unlock()

	}
	return
}

func (rf *Raft) AppendEntries(request AppendEntriesArgs, response *AppendEntriesReply) { //this is the handler that is invoke when a certain node receives an append entries from the leader
	fmt.Println("server number,", rf.me, ",REceived append entries ", request)
	fmt.Println("server number", rf.me, "my log is", rf.log)
	//fmt.Println("my current term is", rf.currentTerm)
	//fmt.Println("request term is", request.Term)
	rf.mu.Lock()
	//fmt.Println("server number,", rf.me, "attained lock ", request)
	if rf.currentTerm <= request.Term || rf.myRole == "Candidate" { //if you receive the append entries from a node with term grater than or equal to your term, then you should accept it as a leader
		fmt.Println("server number, ", rf.me, "accepting ping from, ", request)
		//over here do all processing
		//you need to send back your term and your if it is a success or not

		myLogLength := len(rf.log)
		//prev log index would exist if it is loesser than myloglegth -1
		fmt.Println("server number", rf.me, "my loglength is", myLogLength)

		if myLogLength > request.PrevLogIndex { //first check prevlog index, if it exists
			fmt.Println("server number", rf.me, "previous log index exists")
			if request.PrevLogTerm == rf.log[request.PrevLogIndex].Term && request.PrevLogIndex == myLogLength-1 { //if it exists then check the term and compare it its term to the leader term
				//if length is more than 1 of entries, then u need to DROP all entries from 1+prevlog index till the end... then append all teh entries

				rf.updateStatus <- "Ping" //ping mis used to differentiate a heartbeat from a request vote message
				fmt.Println("server number ", rf.me, " setting temp term to: ", rf.tempTerm, " temp vote to : ", rf.tempVote, "in append entries")
				rf.tempTerm = request.Term //indicate that you are changing your term /vote
				rf.tempVote = request.LeaderId
				response.Term = request.Term
				rf.mu.Unlock()
				fmt.Println("server number ", rf.me, " pushing signal to signal channel in append entries")
				response.Success = true
				j := 1
				for j < len(request.Entries) {
					rf.log = append(rf.log, request.Entries[j])
					j = j + 1

				}
				fmt.Println("server number", rf.me, "in case 1")
				rf.signalChannel <- "signal" //send a signal to the role routines (LeaderMode, CandidateMode, FollowerMode) running that they can access the temp variables

			} else { //else you need to remove the entries from the  prevlogindex and till the end and return false
				rf.log = rf.log[:request.PrevLogIndex]
				rf.updateStatus <- "Ping" //ping mis used to differentiate a heartbeat from a request vote message
				fmt.Println("server number ", rf.me, " setting temp term to: ", rf.tempTerm, " temp vote to : ", rf.tempVote, "in append entries")
				rf.tempTerm = request.Term //indicate that you are changing your term /vote
				rf.tempVote = request.LeaderId
				response.Term = request.Term
				rf.mu.Unlock()
				fmt.Println("server number ", rf.me, " pushing signal to signal channel in append entries")
				response.Success = false
				fmt.Println("server number", rf.me, "in case 2")
				rf.signalChannel <- "signal"

			}

		} else { //i fprevlog index does not exist
			//find out the length of the entries array
			lenEntries := len(request.Entries)
			if lenEntries > 0 { // if the length is greater than 0
				diff := lenEntries - 1
				x := request.PrevLogIndex - diff
				fmt.Println("Server number,", rf.me, "x is", x)
				//check if there is a value at x or not
				if myLogLength > x || x == 0 {
					//check the matching with first entry in the entries array
					if x == 0 {
						//if matches then append all entries, and return true
						rf.updateStatus <- "Ping" //ping mis used to differentiate a heartbeat from a request vote message
						fmt.Println("server number ", rf.me, " setting temp term to: ", rf.tempTerm, " temp vote to : ", rf.tempVote, "in append entries")
						rf.tempTerm = request.Term //indicate that you are changing your term /vote
						rf.tempVote = request.LeaderId
						response.Term = request.Term
						rf.mu.Unlock()
						fmt.Println("server number ", rf.me, " pushing signal to signal channel in append entries")
						response.Success = true
						fmt.Println("server number", rf.me, "in case 3")

						j := 1
						if len(rf.log) == 0 {
							j = 0
						}
						for j < len(request.Entries) {
							rf.log = append(rf.log, request.Entries[j])
							j = j + 1

						}

						rf.signalChannel <- "signal"

					} else if (request.Entries[0].Term == rf.log[x].Term) && (request.Entries[0].Command == rf.log[x].Command) {

						//if matches then append all entries, and return true
						rf.updateStatus <- "Ping" //ping mis used to differentiate a heartbeat from a request vote message
						fmt.Println("server number ", rf.me, " setting temp term to: ", rf.tempTerm, " temp vote to : ", rf.tempVote, "in append entries")
						rf.tempTerm = request.Term //indicate that you are changing your term /vote
						rf.tempVote = request.LeaderId
						response.Term = request.Term

						fmt.Println("server number ", rf.me, " pushing signal to signal channel in append entries")
						response.Success = true
						fmt.Println("server number", rf.me, "in case 3")

						j := 1
						for j < len(request.Entries) {
							rf.log = append(rf.log, request.Entries[j])
							j = j + 1

						}
						rf.mu.Unlock()

						rf.signalChannel <- "signal"

					} else {
						rf.log = rf.log[0:request.PrevLogIndex]
						rf.updateStatus <- "Ping" //ping mis used to differentiate a heartbeat from a request vote message
						fmt.Println("server number ", rf.me, " setting temp term to: ", rf.tempTerm, " temp vote to : ", rf.tempVote, "in append entries")
						rf.tempTerm = request.Term //indicate that you are changing your term /vote
						rf.tempVote = request.LeaderId
						response.Term = request.Term
						rf.mu.Unlock()
						fmt.Println("server number ", rf.me, " pushing signal to signal channel in append entries")
						response.Success = false
						fmt.Println("server number", rf.me, "in case 4")
						rf.signalChannel <- "signal"
						//then drop entries and return false
					}
				} else { //if there is no value at x

					rf.updateStatus <- "Ping" //ping mis used to differentiate a heartbeat from a request vote message
					fmt.Println("server number ", rf.me, " setting temp term to: ", rf.tempTerm, " temp vote to : ", rf.tempVote, "in append entries")
					rf.tempTerm = request.Term //indicate that you are changing your term /vote
					rf.tempVote = request.LeaderId
					response.Term = request.Term
					rf.mu.Unlock()
					fmt.Println("server number ", rf.me, " pushing signal to signal channel in append entries")
					response.Success = false
					fmt.Println("server number", rf.me, "in case 5")
					rf.signalChannel <- "signal"
					//return false
				}

			} else { // if the length is 0 of entries then u just return false
				//ur supposed to return false
				rf.updateStatus <- "Ping" //ping mis used to differentiate a heartbeat from a request vote message
				fmt.Println("server number ", rf.me, " setting temp term to: ", rf.tempTerm, " temp vote to : ", rf.tempVote, "in append entries")
				rf.tempTerm = request.Term //indicate that you are changing your term /vote
				rf.tempVote = request.LeaderId
				response.Term = request.Term
				rf.mu.Unlock()
				fmt.Println("server number ", rf.me, " pushing signal to signal channel in append entries")
				response.Success = false
				fmt.Println("server number", rf.me, "in case 6")
				rf.signalChannel <- "signal"

			}

		}

	} else { //otherwise just reject and do not send any signal to role routines this means that ur term is greater

		fmt.Println("server number, ", rf.me, "rejecting ping from, ", request, " because my term is greater")
		rf.mu.Unlock() //
		rf.mu.Lock()
		response.Term = rf.currentTerm
		rf.mu.Unlock()
		fmt.Println("server number", rf.me, "in case 7")
		response.Success = false

	}

	if response.Success {
		//fmt.Println("I AM A FOLLOWERRRRRR WHO RETURNED TRUEEE")
		rf.mu.Lock()
		oldCommitIndex := rf.commitIndex
		rf.commitIndex = request.LeaderCommit
		rf.mu.Unlock()
		rf.Commit(oldCommitIndex, rf.commitIndex)
	}
	rf.mu.Lock()
	rf.persist()
	rf.mu.Unlock()
}

// sending routine for requestvote
func (rf *Raft) sendRequestVote(server int, args RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// sending routine for append entries
func (rf *Raft) sendAppendEntries(server int, args AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	//after clling append entries
	//check what repsonse you received
	fmt.Println("The entries request to server ", server, " was,", reply.Success)
	//fmt.Println("The next index for this server was", server, rf.nextIndex[server])

	if reply.Success == false && ok {
		fmt.Println("trynna attain lock", server)
		rf.mu.Lock()
		if rf.nextIndex[server] != 0 {
			//rf.mu.Lock()
			rf.nextIndex[server]--

			fmt.Println("The next index for this server,", server, "now decremented is", rf.nextIndex[server])
		} else {
			fmt.Println("The next index for this server,", server, "is", rf.nextIndex[server])

		}
		rf.mu.Unlock()
	} else if reply.Success == true && ok {
		rf.mu.Lock()
		if len(args.Entries) > 1 {
			rf.nextIndex[server] = rf.nextIndex[server] + len(args.Entries) - 1
		} else if len(args.Entries) == 1 {
			rf.nextIndex[server] = rf.nextIndex[server] + len(args.Entries)
		}
		rf.matchIndex[server] = rf.nextIndex[server] - 1

		rf.mu.Unlock()
		//give away the lock to prevent deadlock and then call a fucntion
		rf.DetectCommit()
		fmt.Println("The next index for this server,", server, "now incremented is", rf.nextIndex[server])
		fmt.Println("The match index for this server,", server, "now is", rf.matchIndex[server])

	} else {
		fmt.Println("NOT OKAY!", server)
	}

	//only if it returns true then u update the match index to 1 lesser than the updated next index
	//then after updating thematch index for each and every one of the servers, go on to a functio
	//in that function update the commit index, commit the thing and then update the rf.leadercommit

	return ok
}

func (rf *Raft) DetectCommit() {
	//first find a temp commit index that can be updated to
	//create an empty array
	//get your previous commit index, and count the number of nodes that have match index greater than that
	//whenever you update the match index
	freq := make(map[int]int)
	rf.mu.Lock()
	fmt.Println("server number", rf.me, "my match index is ", rf.matchIndex)
	for _, num := range rf.matchIndex {
		freq[num] = freq[num] + 1
		if num > rf.commitIndex {
			if freq[num] >= (len(rf.peers)-1)/2 {
				//update the commit index here first
				oldCommitIndex := rf.commitIndex
				rf.commitIndex = num
				//then u should call a function that would commmit
				fmt.Println("\n\n-----------------------------Server number,", rf.me, "commit index is ", rf.commitIndex)
				//commit here the values from the (old match leader commit till the new match index which will be the value of num
				//make a function that would ocmmit from the old commit index itll the updated commited indec
				rf.mu.Unlock()
				rf.Commit(oldCommitIndex, rf.commitIndex)
				rf.mu.Lock()

			}
		}
	}
	fmt.Println("Server number", rf.me, "freq is", freq)

	rf.mu.Unlock()

}

func (rf *Raft) Commit(startIndex int, endIndex int) {
	//you dont need to lock anything here cox only one thread will have access to this anyway ideally
	//from 1 + start index till the endindex included, you need to commiit
	fmt.Println("server number", rf.me, " with a start index", startIndex, "and end index", endIndex)

	for i := startIndex + 1; i <= endIndex; i++ {
		x := ApplyMsg{Index: i, Command: rf.log[i].Command}
		fmt.Println("INSIDE COMMIITTT", rf.log)
		fmt.Println("start index is", startIndex)
		fmt.Println("end index is", endIndex)
		fmt.Println("apply message issis", x)
		rf.applyChannel <- ApplyMsg{Index: i, Command: rf.log[i].Command}
	}
	rf.mu.Lock()
	rf.persist()
	rf.mu.Unlock()
}

// start the server
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := false
	rf.mu.Lock()

	if rf.myRole == "Leader" {

		index = rf.nextIndex[rf.me]
		//you should increase your next index here
		rf.nextIndex[rf.me] = rf.nextIndex[rf.me] + 1
		term = rf.currentTerm
		//change the
		rf.prevLogIndex = rf.prevLogIndex + 1
		fmt.Println("server number", rf.me, "the lof i make is ,", Log{term, command})
		rf.log = append(rf.log, Log{term, command})
		rf.persist()
		rf.mu.Unlock()
		return index, term, true

	} else {
		rf.mu.Unlock()
		return index, term, isLeader
	}
}

func (rf *Raft) Kill() {

}

// Make and initialize all Raft state variables
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	rf.signalChannel = make(chan string)
	rf.updateStatus = make(chan string)
	rf.signalElections = make(chan string)
	rf.myRole = "Follower" //your initial role is a follower
	rf.currentTerm = 0     //your iniyial term is 0 / here
	rf.votedFor = -1       //your votedfor is nil (-1) initially
	rf.log = append(rf.log, Log{rf.currentTerm, -1})

	rf.tempVote = -1
	rf.tempTerm = -1

	//////PART 3
	rf.prevLogIndex = 0 //the previous log index is 0
	//this is done as a filler
	fmt.Println("the log at make is", rf.log)
	rf.lastApplied = 0        //initialized to 0 and increases monotonically
	rf.commitIndex = 0        //initialized to 0 and increases monotonically
	rf.nextIndex = []int{}    //this is an array of the next index
	rf.matchIndex = []int{}   //this is an array of the match index
	rf.applyChannel = applyCh //make(chan ApplyMsg)
	i := 0
	for i < len(rf.peers) {
		//add -1
		rf.nextIndex = append(rf.nextIndex, 1)   //fill it with -1 for all values
		rf.matchIndex = append(rf.matchIndex, 1) //fill it with -1 for all values
		i = i + 1
	}
	//for all nextIndex and matchIndex, add -1
	//over here read persis
	rf.readPersist(persister.ReadRaftState())
	go rf.followerMode() //start the role routine as a follower
	fmt.Println("server number", rf.me, "running for follower")
	//rf.readPersist(persister.ReadRaftState())

	return rf
}

//role routine number 1 - followermode //run as a follower

func (rf *Raft) followerMode() {
	rand.Seed(time.Now().UnixNano())
	min := 300
	max := 600
	timeout := rand.Intn(max-min+1) + min //get a random timeout value

	rf.mu.Lock()
	rf.myRole = "Follower"

	rf.mu.Unlock()
	timeoutChannel := time.NewTimer(time.Duration(timeout) * time.Millisecond) //set the timer
	for {
		fmt.Println("server number ", rf.me, "running follower mode")

		select {

		case newStatus := <-rf.updateStatus: //if you receive a signal
			{
				if newStatus == "Follower" { //this means u received a request vote and you want to give vote
					fmt.Println("server number ", rf.me, "received follower from update status channel in follower mode")

					_ = <-rf.signalChannel
					fmt.Println("server number ", rf.me, "received signal from signal channel in follower mode")

					rf.mu.Lock()
					if rf.tempTerm > rf.currentTerm {
						fmt.Println("server number ", rf.me, " temp term is greater than my term in follower mode")

						if rf.tempVote != -1 {
							fmt.Println("server number ", rf.me, " temp vote is not -1 in follower mode")

							rf.votedFor = rf.tempVote

						}
						fmt.Println("server number ", rf.me, " setting current term to temp term in follower mode")

						rf.currentTerm = rf.tempTerm

						rf.tempVote = -1
						rf.tempTerm = -1

					}
					rf.persist()
					rf.mu.Unlock()

					/////////////
					rand.Seed(time.Now().UnixNano())
					min := 300
					max := 600
					timeout := rand.Intn(max-min+1) + min
					fmt.Println("server number ", rf.me, " resetting channel")
					timeoutChannel.Reset(time.Duration(timeout) * time.Millisecond)

				} else if newStatus == "Ping" { //this means you received an append entries and you are supposed to reset your timer
					fmt.Println("server number ", rf.me, "received ping from update status channel in follower mode")

					_ = <-rf.signalChannel
					fmt.Println("server number ", rf.me, "received signal from signal channel in follower mode")

					rf.mu.Lock()
					fmt.Println("server number ", rf.me, "making tempvote and tempterm is -1")

					rf.tempVote = -1
					rf.tempTerm = -1
					rf.mu.Unlock()

					rand.Seed(time.Now().UnixNano())
					min := 300
					max := 600
					timeout := rand.Intn(max-min+1) + min
					fmt.Println("server number ", rf.me, " resetting channel")
					timeoutChannel.Reset(time.Duration(timeout) * time.Millisecond)

				}

			}
		case <-timeoutChannel.C: //you have timed out and didnt reeive any ping, so just transition into candidate routine
			{
				fmt.Println("server number", rf.me, "running for candidate")
				rf.candidateMode()
				return

			}

		}

	}

}

//role routine number 2 - candidatemode //run as a candidate

func (rf *Raft) candidateMode() {

	rand.Seed(time.Now().UnixNano())
	min := 400
	max := 700
	timeout := rand.Intn(max-min+1) + min                                      //get a random timeout
	timeoutChannel := time.NewTimer(time.Duration(timeout) * time.Millisecond) //set a timeout channel
	rf.mu.Lock()
	rf.myRole = "Candidate"
	rf.mu.Unlock()

	for {
		fmt.Println("server number ", rf.me, "starting elections")

		go rf.startElections() //spawn a routine to strat elections simultaneously

		select {

		case electionResult := <-rf.updateStatus:
			{
				fmt.Println("server number ", rf.me, "received from update status channel in candidate mode")

				if electionResult == "Leader" { //you got the intimation to be a leader

					fmt.Println("server number , ", rf.me, "received leader from update status channel in candidate mode")
					rf.mu.Lock()
					if rf.myRole == "Follower" || rf.myRole == "Leader" { //make sure that you arent already the leader/ follower and this is not stale result
						fmt.Println("server number , ", rf.me, "my role is f or l in candidate mode")

						rf.mu.Unlock() //if stale result due to latency, then just ignore
						return
					}
					rf.myRole = "Leader" //otherwise if not stale then just be a leader
					fmt.Println("server number", rf.me, "trynna be leader")

					//change all the next indeices and the match index here
					//your log is the true log,hence get the length of your log
					//suppose your log is of length 5, hence the next index of you and every other server should be your current length
					//the match index is kept as 0 for all servers
					rf.prevLogIndex = len(rf.log) - 1
					ff := len(rf.log)
					rf.mu.Unlock()
					j := 0
					for j < len(rf.peers) {
						myLogLength := ff
						rf.mu.Lock()                  //len(rf.log)
						rf.nextIndex[j] = myLogLength //next index should be your log's length
						rf.matchIndex[j] = 0
						rf.mu.Unlock() //match index should be 0
						j = j + 1

					}
					leaderMode(rf)

					return
				}

				if electionResult == "Follower" { // if you receive a request for vote from a node with higher term then give that node the vote and go back to being a folllower

					_ = <-rf.signalChannel
					rf.mu.Lock()
					if rf.tempVote != -1 {

						rf.votedFor = rf.tempVote

					}
					rf.currentTerm = rf.tempTerm
					rf.persist()
					rf.mu.Unlock()
					rf.followerMode()
					fmt.Println("server number", rf.me, "transitioning to a follower because it received request for votes from a node with greater term ")
					rf.tempVote = -1
					rf.tempTerm = -1

					return

				} else if electionResult == "Ping" { //if you receive ping from a node while you are a foolish candidate then reset your timer

					_ = <-rf.signalChannel

					rf.mu.Lock()
					rf.tempVote = -1
					rf.tempTerm = -1
					rf.mu.Unlock()
					rand.Seed(time.Now().UnixNano())
					min := 400
					max := 700
					timeout := rand.Intn(max-min+1) + min
					timeoutChannel.Reset(time.Duration(timeout) * time.Millisecond)

				}
			}
		case <-timeoutChannel.C: // otherwise if you receive no signal then just reset the timer and start elections again
			{

				rand.Seed(time.Now().UnixNano())
				min := 400
				max := 700
				timeout := rand.Intn(max-min+1) + min
				timeoutChannel.Reset(time.Duration(timeout) * time.Millisecond)

			}

		}

	}
}

//role routine number 3 - leaderMode //run as a leader

func leaderMode(rf *Raft) {
	for {

		timeoutChannel := time.NewTimer(time.Duration(100) * time.Millisecond) //create a timeout channel for sending heartbeats
		select {

		case newStatus := <-rf.updateStatus:
			{
				if newStatus == "Follower" || newStatus == "Ping" { //as a leader if someone else pings you with higher term or if someone requests vote with higher term then go back to being a follower
					_ = <-rf.signalChannel

					if newStatus == "Follower" {
						rf.mu.Lock()
						rf.votedFor = rf.tempVote
						rf.mu.Unlock()
					}
					rf.mu.Lock()
					rf.currentTerm = rf.tempTerm
					//persist here
					rf.persist()
					rf.mu.Unlock()
					rf.tempVote = -1
					rf.tempTerm = -1

					rf.followerMode()
					return

				}
			}

		case <-timeoutChannel.C: //if you timeout
			{
				var myRole bool
				_, myRole = rf.GetState() //and you are still the leader

				rf.mu.Lock()
				currentNumberAlive := len(rf.peers)
				rf.mu.Unlock()
				if myRole {

					for server := 0; server < currentNumberAlive; server++ {
						var response AppendEntriesReply
						if server != rf.me {

							_, myRole = rf.GetState()

							if myRole {

								entriesToSend := []Log{}
								///inside entries to send, append the log entries after from next index till end
								rf.mu.Lock()
								entryNumber := rf.nextIndex[server]
								lenrf := len(rf.log)
								rf.mu.Unlock()
								for entryNumber < lenrf { //len(rf.log) {
									//you need to keep on appending entries
									entriesToSend = append(entriesToSend, rf.log[entryNumber])
									entryNumber = entryNumber + 1

								}
								rf.mu.Lock()
								leadCommit := rf.commitIndex
								leadTerm := rf.currentTerm
								leadPrev := rf.prevLogIndex
								leadPrevTerm := rf.log[rf.prevLogIndex].Term
								rf.mu.Unlock()

								request := AppendEntriesArgs{
									LeaderId:     rf.me,
									Term:         leadTerm,
									PrevLogIndex: leadPrev,
									PrevLogTerm:  leadPrevTerm,
									Entries:      entriesToSend,
									LeaderCommit: leadCommit}

								//before you are sending append entries .... make edits here
								//after you have made changes to appen entries, send those

								go rf.sendAppendEntries(server, request, &response) // then send append entries in go routines

							}

						}
					}

					timeoutChannel.Reset(time.Duration(100) * time.Millisecond)

				}

			}
		}

	}
}

func (rf *Raft) startElections() { //routine for running elections

	startElections := false
	rf.mu.Lock()
	if rf.myRole == "Candidate" { //check again to prevent stale roles from initiating elections
		startElections = true
		rf.mu.Unlock()

	} else {
		startElections = false
		rf.mu.Unlock()

	}

	if startElections == true {
		rf.mu.Lock()
		rf.votedFor = rf.me
		//rf.mu.Lock()
		rf.currentTerm = rf.currentTerm + 1
		rf.persist()
		//rf.mu.Unlock()
		electionResults := []int{} //make an array for storing the results of the elctions from each ofthe peers
		responseObjects := []RequestVoteReply{}
		requestVotesObject := RequestVoteArgs{ //this is the args sent to the peers
			Term:         rf.currentTerm,
			CandidateId:  rf.me,
			LastLogIndex: len(rf.log) - 1,
			LastLogTerm:  rf.log[len(rf.log)-1].Term}

		numberServers := len(rf.peers)

		i := 0
		//make sure that all are false as results initially except your own
		for i < len(rf.peers) {
			if i == rf.me {
				electionResults = append(electionResults, 1)

			} else {
				electionResults = append(electionResults, 0)

			}
			var responseObject RequestVoteReply
			responseObjects = append(responseObjects, responseObject)
			i = i + 1

		}

		rf.mu.Unlock()
		for serverNumber := 0; serverNumber < numberServers; serverNumber++ {
			if serverNumber != rf.me {
				fmt.Println("server number", rf.me, "requesting from", serverNumber)
				go rf.requestVoteSender(&electionResults, serverNumber, responseObjects, requestVotesObject)
				//send request a routine and share the same results array
				//hence the last routine spawned will be able to touch quorum provided the count is sufficient if so and hence only that routine would signal that you should become a leader

			}

		}

	}

}

func (rf *Raft) requestVoteSender(electionResults *[]int, serverNumber int, responseObjects []RequestVoteReply, requestVotesObject RequestVoteArgs) {

	ok := rf.sendRequestVote(serverNumber, requestVotesObject, &responseObjects[serverNumber])

	rf.mu.Lock()
	if responseObjects[serverNumber].VoteGranted {
		if ok == true {
			(*electionResults)[serverNumber] = 1
		} else {
			(*electionResults)[serverNumber] = 0
		}

	} else {
		(*electionResults)[serverNumber] = 0
	}

	countVote := 0
	fmt.Println("server number", rf.me, "election results are", electionResults)
	for i := 0; i < len(rf.peers); i++ {
		if (*electionResults)[i] == 1 {
			countVote = countVote + 1
		}
	}

	rf.mu.Unlock()
	quorum := len(rf.peers) / 2
	fmt.Println("server number", rf.me, "count is ", countVote)
	if countVote > (quorum) {
		rf.updateStatus <- "Leader"

	}
}
