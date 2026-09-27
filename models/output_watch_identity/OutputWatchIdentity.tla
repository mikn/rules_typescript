------------------------ MODULE OutputWatchIdentity ------------------------
EXTENDS Naturals

CONSTANT Mode
VARIABLES parentEpoch, leafEpoch, fileEpoch, present, handles, retired, pending, observed, quiet
vars == <<parentEpoch, leafEpoch, fileEpoch, present, handles, retired, pending, observed, quiet>>

ASSUME Mode \in {"Directory", "FileInode", "LeafOnly", "Unfenced"}
Epochs == 0..2
Subjects == {"anchor", "parent", "leaf", "file"}
None == [subject |-> "none", inode |-> <<>>]
Facts == [parent |-> parentEpoch, leaf |-> leafEpoch, file |-> fileEpoch, present |-> present]
Identity(subject) == CASE subject = "anchor" -> <<0>>
                       [] subject = "parent" -> <<parentEpoch>>
                       [] subject = "leaf" -> <<parentEpoch, leafEpoch>>
                       [] OTHER -> <<parentEpoch, leafEpoch, fileEpoch>>
Directories == {"anchor", "parent"} \cup (IF present THEN {"leaf"} ELSE {})
Desired == CASE Mode = "FileInode" -> Directories \cup (IF present THEN {"file"} ELSE {})
             [] Mode = "LeafOnly" -> IF present THEN {"leaf"} ELSE {}
             [] OTHER -> Directories
Attached(subject) == IF handles[subject] = None THEN FALSE
                     ELSE handles[subject].inode = Identity(subject)
Binding(subject) == [subject |-> subject, inode |-> Identity(subject)]
Reconciled == [subject \in Subjects |->
    IF subject \notin Desired THEN None
    ELSE IF Attached(subject) \/ (Mode = "FileInode" /\ subject = "file" /\ handles[subject] # None)
         THEN handles[subject] ELSE Binding(subject)]

Init == /\ parentEpoch = 0 /\ leafEpoch = 0 /\ fileEpoch = 0
        /\ present = TRUE
        /\ handles = [subject \in Subjects |-> None]
        /\ retired = {}
        /\ pending = TRUE
        /\ observed = Facts
        /\ quiet = FALSE

Publish == /\ ~quiet /\ present /\ fileEpoch < 2
           /\ fileEpoch' = fileEpoch + 1
           /\ pending' = (pending \/ Attached(IF Mode = "FileInode" THEN "file" ELSE "leaf"))
           /\ UNCHANGED <<parentEpoch, leafEpoch, present, handles, retired, observed, quiet>>

ReplaceParent == /\ ~quiet /\ parentEpoch < 2
                 /\ parentEpoch' = parentEpoch + 1
                 /\ pending' = (pending \/ Attached("anchor") \/ Attached("parent"))
                 /\ UNCHANGED <<leafEpoch, fileEpoch, present, handles, retired, observed, quiet>>

DeleteLeaf == /\ ~quiet /\ present
              /\ present' = FALSE
              /\ pending' = (pending \/ Attached("parent") \/ Attached("leaf") \/ Attached("file"))
              /\ UNCHANGED <<parentEpoch, leafEpoch, fileEpoch, handles, retired, observed, quiet>>

CreateLeaf == /\ ~quiet /\ ~present /\ leafEpoch < 2
              /\ present' = TRUE /\ leafEpoch' = leafEpoch + 1
              /\ pending' = (pending \/ Attached("parent"))
              /\ UNCHANGED <<parentEpoch, fileEpoch, handles, retired, observed, quiet>>

Reconcile == /\ pending
             /\ handles' = Reconciled
             /\ retired' = retired \cup
                    {handles[subject] : subject \in
                        {s \in Subjects : handles[s] # None /\ handles[s] # Reconciled[s]}}
             /\ observed' = Facts
             /\ pending' = FALSE
             /\ UNCHANGED <<parentEpoch, leafEpoch, fileEpoch, present, quiet>>

LateError(handle) ==
    /\ handle \in retired
    /\ handles' = IF Mode = "Unfenced" \/ handles[handle.subject] = handle
                  THEN [handles EXCEPT ![handle.subject] = None] ELSE handles
    /\ retired' = retired \ {handle}
    /\ UNCHANGED <<parentEpoch, leafEpoch, fileEpoch, present, pending, observed, quiet>>

FinishPublishing == /\ ~quiet /\ quiet' = TRUE
                    /\ UNCHANGED <<parentEpoch, leafEpoch, fileEpoch, present,
                                   handles, retired, pending, observed>>
Done == /\ quiet /\ ~pending /\ UNCHANGED vars

Next == Publish \/ ReplaceParent \/ DeleteLeaf \/ CreateLeaf \/ Reconcile \/ FinishPublishing \/ Done \/
        (\E handle \in retired : LateError(handle))
Spec == Init /\ [][Next]_vars /\ WF_vars(Reconcile)

TypeOK == /\ parentEpoch \in Epochs /\ leafEpoch \in Epochs /\ fileEpoch \in Epochs
          /\ present \in BOOLEAN /\ pending \in BOOLEAN /\ quiet \in BOOLEAN
          /\ DOMAIN handles = Subjects
          /\ observed \in [parent : Epochs, leaf : Epochs, file : Epochs, present : BOOLEAN]
FreshWhenSettled == ~pending => observed = Facts
ReconciledIdentity == ~pending =>
    /\ \A subject \in Directories : Attached(subject)
    /\ \A subject \in Subjects \ Directories : handles[subject] = None
QuiescentConvergence == quiet ~> (observed = Facts)
=============================================================================
