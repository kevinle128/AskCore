---------------------------- MODULE ToolBatch ----------------------------
(* One tool batch of internal/agent: the coordinator loop of runToolBatch      *)
(* (tool_coordinator.go), its worker goroutines, an Abort from another         *)
(* goroutine, one failed log write or event publish, the deferred drain, and   *)
(* repairTools (tool_repair.go), which agent.go runs when the run returns an   *)
(* error. Each action names the Go lines it stands for.                        *)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS N,      \* tool calls in the assistant message
          Limit   \* Config.MaxParallelTools

Calls == 1..N
Texts == {"ok", "err", "blocked", "aborted", "beforeDispatch", "unknown", "notStarted"}
NoText == "none"

VARIABLES
  pc,          \* where the driver goroutine is
  safe,        \* calls whose tool says ConcurrencySafe for these arguments
  aborted,     \* l.ctx is cancelled (Abort or Dispose)
  cancelled,   \* the batch ctx is cancelled by the deferred cancel()
  failed,      \* runToolBatch returns an error (a write or a publish failed)
  next,        \* `next`: calls prepared and decided
  committed,   \* `committed`: calls whose result is committed
  active,      \* `active`: workers started and not yet accepted
  exclFlag,    \* `exclusive`: an exclusive body is running
  prepared, ready, after, excl, outcome, \* toolSlot fields
  worker,      \* worker goroutine of each call
  bodyRan,     \* `invoked`: tool.Execute was called
  doneQ,       \* the `done` channel, buffered to Limit
  notify,      \* b.notify, buffered to 1
  recorded,    \* a sessions.ToolCall entry for the call is in the log
  results      \* ToolResultMessage entries in log order: <<call, text>>

vars == <<pc, safe, aborted, cancelled, failed, next, committed, active, exclFlag,
          prepared, ready, after, excl, outcome, worker, bodyRan, doneQ, notify,
          recorded, results>>

Answered(i) == \E k \in 1..Len(results) : results[k][1] = i

Init ==
  /\ pc = "collect"
  /\ safe \in SUBSET Calls
  /\ aborted = FALSE /\ cancelled = FALSE /\ failed = FALSE
  /\ next = 0 /\ committed = 0 /\ active = 0 /\ exclFlag = FALSE
  /\ prepared = [i \in Calls |-> FALSE]
  /\ ready = [i \in Calls |-> FALSE]
  /\ after = [i \in Calls |-> FALSE]
  /\ excl = [i \in Calls |-> FALSE]
  /\ outcome = [i \in Calls |-> NoText]
  /\ worker = [i \in Calls |-> "none"]
  /\ bodyRan = [i \in Calls |-> FALSE]
  /\ doneQ = << >>
  /\ notify = FALSE
  /\ recorded = [i \in Calls |-> FALSE]
  /\ results = << >>

(* A failed write or publish: runToolBatch returns err; the deferred cancel() *)
(* runs and the drain loop waits for every active worker.                     *)
FailTo == /\ failed' = TRUE /\ cancelled' = TRUE /\ pc' = "drain"

(* accept(d) in runToolBatch *)
AcceptHead ==
  LET d == Head(doneQ) IN
  /\ ready' = [ready EXCEPT ![d[1]] = TRUE]
  /\ outcome' = [outcome EXCEPT ![d[1]] = d[2]]
  /\ after' = [after EXCEPT ![d[1]] = TRUE]
  /\ active' = active - 1
  /\ exclFlag' = IF excl[d[1]] THEN FALSE ELSE exclFlag
  /\ doneQ' = Tail(doneQ)

(* Abort or Dispose cancels l.ctx from another goroutine at any time. *)
Abort ==
  /\ ~aborted /\ pc # "done"
  /\ aborted' = TRUE
  /\ UNCHANGED <<pc, safe, cancelled, failed, next, committed, active, exclFlag, prepared,
                 ready, after, excl, outcome, worker, bodyRan, doneQ, notify, recorded, results>>

(* The non-blocking `select { case d := <-done ... default: goto collected }`. *)
Collect ==
  /\ pc = "collect"
  /\ IF doneQ # << >>
       THEN AcceptHead /\ UNCHANGED pc
       ELSE pc' = "commit" /\ UNCHANGED <<ready, outcome, after, active, exclFlag, doneQ>>
  /\ UNCHANGED <<safe, aborted, cancelled, failed, next, committed, prepared, excl, worker,
                 bodyRan, notify, recorded, results>>

(* The `for committed < next && slots[committed].ready` loop: progress, afterTool, *)
(* emitToolEnd, emitMessage (MessageStart, commit, MessageEnd).                     *)
CommitNext ==
  LET c == committed + 1
      final == IF after[c] /\ aborted /\ outcome[c] = "ok" THEN "aborted" ELSE outcome[c]
  IN
  /\ pc = "commit" /\ committed < next /\ ready[c]
  /\ \/ /\ results' = Append(results, <<c, final>>)
        /\ committed' = committed + 1
        /\ UNCHANGED <<pc, cancelled, failed>>
     \/ \* progress, emitToolEnd, MessageStart or the append fails: no result entry
        /\ ~failed /\ FailTo
        /\ UNCHANGED <<results, committed>>
     \/ \* MessageEnd fails after the result entry is in the log
        /\ ~failed /\ FailTo
        /\ results' = Append(results, <<c, final>>)
        /\ UNCHANGED committed
  /\ UNCHANGED <<safe, aborted, next, active, exclFlag, prepared, ready, after, excl, outcome,
                 worker, bodyRan, doneQ, notify, recorded>>

(* After the commit loop: `if committed == len(calls) { break }`, or go on to start. *)
CommitDone ==
  /\ pc = "commit"
  /\ ~(committed < next /\ ready[committed + 1])
  /\ IF committed = N
       THEN pc' = "drain" /\ cancelled' = TRUE
       ELSE pc' = "start" /\ UNCHANGED cancelled
  /\ UNCHANGED <<safe, aborted, failed, next, committed, active, exclFlag, prepared, ready, after,
                 excl, outcome, worker, bodyRan, doneQ, notify, recorded, results>>

CanStart == next < N /\ ~exclFlag /\ active < Limit

(* b.prepare: commit the ToolCall entry, emitToolStart, the ctx check, BeforeTool, *)
(* the ctx check after it. Validation errors behave like "blocked" (an error result). *)
Prepare ==
  LET s == next + 1 IN
  /\ pc = "start" /\ CanStart /\ ~prepared[s]
  /\ \/ \* the ToolCall append fails
        /\ ~failed /\ FailTo
        /\ UNCHANGED <<recorded, prepared, ready, after, outcome>>
     \/ \* emitToolStart fails after the ToolCall entry is in the log
        /\ ~failed /\ FailTo
        /\ recorded' = [recorded EXCEPT ![s] = TRUE]
        /\ UNCHANGED <<prepared, ready, after, outcome>>
     \/ /\ recorded' = [recorded EXCEPT ![s] = TRUE]
        /\ prepared' = [prepared EXCEPT ![s] = TRUE]
        /\ pc' = "dispatch"
        /\ UNCHANGED <<cancelled, failed>>
        /\ IF aborted
             THEN /\ ready' = [ready EXCEPT ![s] = TRUE]
                  /\ outcome' = [outcome EXCEPT ![s] = "beforeDispatch"]
                  /\ UNCHANGED after
             ELSE \E gate \in {"allow", "block", "cancel"} :
                    IF gate = "allow"
                      THEN UNCHANGED <<ready, outcome, after>>
                      ELSE /\ ready' = [ready EXCEPT ![s] = TRUE]
                           /\ after' = [after EXCEPT ![s] = TRUE]
                           /\ outcome' = [outcome EXCEPT ![s] =
                                            IF gate = "block" THEN "blocked" ELSE "beforeDispatch"]
  /\ UNCHANGED <<safe, aborted, next, committed, active, exclFlag, excl, worker, bodyRan, doneQ,
                 notify, results>>

(* A call prepared in an earlier pass goes straight to the dispatch decision. *)
Resume ==
  /\ pc = "start" /\ CanStart /\ prepared[next + 1]
  /\ pc' = "dispatch"
  /\ UNCHANGED <<safe, aborted, cancelled, failed, next, committed, active, exclFlag, prepared,
                 ready, after, excl, outcome, worker, bodyRan, doneQ, notify, recorded, results>>

NoStart ==
  /\ pc = "start" /\ ~CanStart
  /\ pc' = "wait"
  /\ UNCHANGED <<safe, aborted, cancelled, failed, next, committed, active, exclFlag, prepared,
                 ready, after, excl, outcome, worker, bodyRan, doneQ, notify, recorded, results>>

(* The rest of the start block: ready -> next++; l.ctx cancelled -> before dispatch;  *)
(* safe, or exclusive with no body active and every earlier call committed -> start. *)
Dispatch ==
  LET s == next + 1
      isExcl == s \notin safe
  IN
  /\ pc = "dispatch"
  /\ CASE ready[s] ->
            /\ next' = next + 1 /\ pc' = "collect"
            /\ UNCHANGED <<active, exclFlag, ready, after, excl, outcome, worker>>
       [] aborted ->
            /\ ready' = [ready EXCEPT ![s] = TRUE]
            /\ after' = [after EXCEPT ![s] = TRUE]
            /\ outcome' = [outcome EXCEPT ![s] = "beforeDispatch"]
            /\ next' = next + 1 /\ pc' = "collect"
            /\ UNCHANGED <<active, exclFlag, excl, worker>>
       [] ~isExcl \/ (active = 0 /\ committed = next) ->
            /\ excl' = [excl EXCEPT ![s] = isExcl]
            /\ worker' = [worker EXCEPT ![s] = "spawned"]
            /\ next' = next + 1 /\ active' = active + 1 /\ exclFlag' = isExcl
            /\ pc' = "collect"
            /\ UNCHANGED <<ready, after, outcome>>
       [] OTHER ->
            /\ excl' = [excl EXCEPT ![s] = isExcl]
            /\ pc' = "wait"
            /\ UNCHANGED <<next, active, exclFlag, ready, after, outcome, worker>>
  /\ UNCHANGED <<safe, aborted, cancelled, failed, committed, prepared, bodyRan, doneQ, notify,
                 recorded, results>>

(* The blocking `select` at the end of the loop body. *)
WaitDone ==
  /\ pc = "wait" /\ doneQ # << >>
  /\ AcceptHead /\ pc' = "collect"
  /\ UNCHANGED <<safe, aborted, cancelled, failed, next, committed, prepared, excl, worker,
                 bodyRan, notify, recorded, results>>

WaitNotify ==
  /\ pc = "wait" /\ notify
  /\ notify' = FALSE
  /\ \/ pc' = "collect" /\ UNCHANGED <<cancelled, failed>>
     \/ ~failed /\ FailTo  \* b.progress fails to publish
  /\ UNCHANGED <<safe, aborted, next, committed, active, exclFlag, prepared, ready, after, excl,
                 outcome, worker, bodyRan, doneQ, recorded, results>>

(* The deferred cancel() and `for active > 0 { <-done; active-- }`. *)
Drain ==
  /\ pc = "drain" /\ active > 0 /\ doneQ # << >>
  /\ doneQ' = Tail(doneQ) /\ active' = active - 1
  /\ UNCHANGED <<pc, safe, aborted, cancelled, failed, next, committed, exclFlag, prepared, ready,
                 after, excl, outcome, worker, bodyRan, notify, recorded, results>>

Return ==
  /\ pc = "drain" /\ active = 0
  /\ pc' = IF failed THEN "repair" ELSE "done"
  /\ UNCHANGED <<safe, aborted, cancelled, failed, next, committed, active, exclFlag, prepared,
                 ready, after, excl, outcome, worker, bodyRan, doneQ, notify, recorded, results>>

(* repairTools: each call of the open turn without a result gets one, in source *)
(* order: "outcome unknown" when a ToolCall entry records it, else "not started". *)
Missing == SelectSeq([k \in 1..N |-> k], LAMBDA i : ~Answered(i))
Repair ==
  /\ pc = "repair"
  /\ results' = results \o [k \in 1..Len(Missing) |->
                   <<Missing[k], IF recorded[Missing[k]] THEN "unknown" ELSE "notStarted">>]
  /\ pc' = "done"
  /\ UNCHANGED <<safe, aborted, cancelled, failed, next, committed, active, exclFlag, prepared,
                 ready, after, excl, outcome, worker, bodyRan, doneQ, notify, recorded>>

Finished == pc = "done" /\ UNCHANGED vars

(* execute(): the once.Do ctx check, then tool.Execute (invoked = true). *)
WorkerCheck(i) ==
  /\ worker[i] = "spawned"
  /\ IF aborted \/ cancelled
       THEN /\ worker' = [worker EXCEPT ![i] = "returned"]
            /\ doneQ' = Append(doneQ, <<i, "beforeDispatch">>)
            /\ UNCHANGED bodyRan
       ELSE /\ worker' = [worker EXCEPT ![i] = "running"]
            /\ bodyRan' = [bodyRan EXCEPT ![i] = TRUE]
            /\ UNCHANGED doneQ
  /\ UNCHANGED <<pc, safe, aborted, cancelled, failed, next, committed, active, exclFlag, prepared,
                 ready, after, excl, outcome, notify, recorded, results>>

(* The body returns (D19: no time bound) and the worker sends on done. A success *)
(* seen after l.ctx is cancelled becomes "aborted".                              *)
WorkerReturn(i) ==
  /\ worker[i] = "running"
  /\ \E r \in {"ok", "err"} :
       doneQ' = Append(doneQ, <<i, IF aborted /\ r = "ok" THEN "aborted" ELSE r>>)
  /\ worker' = [worker EXCEPT ![i] = "returned"]
  /\ UNCHANGED <<pc, safe, aborted, cancelled, failed, next, committed, active, exclFlag, prepared,
                 ready, after, excl, outcome, bodyRan, notify, recorded, results>>

(* updater.send: progress wakes the driver through b.notify. *)
WorkerProgress(i) ==
  /\ worker[i] = "running" /\ ~notify
  /\ notify' = TRUE
  /\ UNCHANGED <<pc, safe, aborted, cancelled, failed, next, committed, active, exclFlag, prepared,
                 ready, after, excl, outcome, worker, bodyRan, doneQ, recorded, results>>

Driver == Collect \/ CommitNext \/ CommitDone \/ Prepare \/ Resume \/ NoStart \/ Dispatch
          \/ WaitDone \/ WaitNotify \/ Drain \/ Return \/ Repair

Next == Driver \/ Abort \/ Finished
        \/ \E i \in Calls : WorkerCheck(i) \/ WorkerReturn(i) \/ WorkerProgress(i)

Spec == Init /\ [][Next]_vars
        /\ WF_vars(Driver)
        /\ \A i \in Calls : WF_vars(WorkerCheck(i)) /\ WF_vars(WorkerReturn(i))

-----------------------------------------------------------------------------
(* Properties. Each is one promise of plan 261006-0933 (D19 revised).          *)

TypeOK ==
  /\ pc \in {"collect", "commit", "start", "dispatch", "wait", "drain", "repair", "done"}
  /\ next \in 0..N /\ committed \in 0..next /\ active \in 0..N
  /\ \A k \in 1..Len(results) : results[k][1] \in Calls /\ results[k][2] \in Texts

Running == {i \in Calls : worker[i] = "running"}

\* At most MaxParallelTools bodies run at once.
PoolBound == Cardinality(Running) <= Limit

\* A body of a tool that is not concurrency-safe runs alone.
ExclusiveAlone == \A i \in Running : excl[i] => Running = {i}

\* An exclusive call starts only after every earlier call is committed (AfterTool
\* included), and no later body starts before the exclusive call is committed.
ExclusiveBarrier ==
  \A i \in Calls : (excl[i] /\ worker[i] # "none") =>
     /\ committed >= i - 1
     /\ \A j \in Calls : (j > i /\ worker[j] # "none") => committed >= i

\* Matrix B2: the ToolCall entry is in the log before the tool body runs.
RecordBeforeBody == \A i \in Calls : bodyRan[i] => recorded[i]

\* No call ever gets two results.
AtMostOneResult == \A i \in Calls : Cardinality({k \in 1..Len(results) : results[k][1] = i}) <= 1

\* Results are in the log in the order of the calls in the assistant message.
ResultsInSourceOrder ==
  \A k, m \in 1..Len(results) : k < m => results[k][1] < results[m][1]

\* A result never claims less than happened: "before dispatch" and "not started"
\* mean the body never ran; "not started" means no ToolCall entry; "outcome
\* unknown" is written only for a recorded call.
TruthfulOutcome ==
  \A k \in 1..Len(results) :
    LET i == results[k][1]  t == results[k][2] IN
    /\ t \in {"beforeDispatch", "notStarted"} => ~bodyRan[i]
    /\ t = "notStarted" => ~recorded[i]
    /\ t = "unknown" => recorded[i]

\* Matrix 50: once l.ctx is cancelled, no tool body starts. This also covers an
\* abort before the batch: it runs no body.
NoBodyAfterAbort == [][\A i \in Calls : (~bodyRan[i] /\ bodyRan'[i]) => ~aborted]_vars

\* D19: when runToolBatch returns, every started body has returned (no time bound),
\* so repair and the next run never overlap a body.
DrainedOnReturn == pc \in {"repair", "done"} => \A i \in Calls : worker[i] \in {"none", "returned"}

\* D19 revised: when the batch (and repair, after an error) is over, every call
\* has a result. With AtMostOneResult: exactly one.
EveryCallAnswered == pc = "done" => \A i \in Calls : Answered(i)

\* The batch always ends, given that every tool body returns.
Terminates == <>(pc = "done")
=============================================================================
