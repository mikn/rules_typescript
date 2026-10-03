--------------------------- MODULE PathsOrder ---------------------------
EXTENDS Integers, Sequences, FiniteSets

CONSTANTS Mode, PreserveFailedProbes
VARIABLES order, override, candidates, available, badStage
vars == <<order, override, candidates, available, badStage>>

ASSUME Mode \in {"Ordered", "Lexical"}
ASSUME PreserveFailedProbes \in BOOLEAN
Orders == {<<2, 1, 3, 4>>, <<1, 2, 3, 4>>}
Stages == {"build", "editor", "installer"}
EffectiveKeys == CASE override = "inherit" -> order
                  [] override = "empty" -> <<>>
                  [] override = "suffix-first" -> <<2, 1, 3, 4>>
                  [] OTHER -> <<1, 2, 3, 4>>
Entry(key, location) ==
    [key |-> key, candidates |->
        [i \in 1..Len(candidates) |->
            [id |-> <<key, candidates[i]>>, location |-> location]]]
Authored == [i \in 1..Len(EffectiveKeys) |-> Entry(EffectiveKeys[i], "authored")]
Elements(sequence) == {sequence[i] : i \in 1..Len(sequence)}
Lexical(entries) ==
    [rank \in 1..Len(entries) |->
        CHOOSE entry \in Elements(entries) :
            Cardinality({other \in Elements(entries) : other.key < entry.key}) = rank - 1]
Project(entries, location) ==
    LET relocated == [i \in 1..Len(entries) |->
            [key |-> entries[i].key, candidates |->
                [j \in 1..Len(entries[i].candidates) |->
                    [id |-> entries[i].candidates[j].id, location |-> location]]]]
    IN IF Mode = "Lexical" /\ location = badStage
       THEN Lexical(relocated) ELSE relocated
Build == Project(Authored, "build")
Editor == Project(Build, "editor")
Installed == Project(Editor, "installer")
Identity(entries) ==
    [i \in 1..Len(entries) |->
        [key |-> entries[i].key, candidates |->
            [j \in 1..Len(entries[i].candidates) |-> entries[i].candidates[j].id]]]

Matches(key, query) == key \in {1, 2, 5} \/ (key = 3 /\ query # "tie") \/ (key = 4 /\ query = "exact")
Priority(key) == CASE key = 4 -> 3 [] key = 3 -> 2 [] OTHER -> 1
First(indices) == CHOOSE i \in indices : \A j \in indices : i <= j
Winner(entries, query) ==
    LET matches == {i \in 1..Len(entries) : Matches(entries[i].key, query)}
        best == {i \in matches : \A j \in matches : Priority(entries[i].key) >= Priority(entries[j].key)}
    IN IF best = {} THEN "missing"
       ELSE LET values == entries[First(best)].candidates
                found == {i \in 1..Len(values) : values[i].id[2] \in available}
            IN IF found = {} THEN "missing" ELSE values[First(found)].id

Init == /\ order \in Orders
        /\ override \in {"inherit", "empty", "suffix-first", "wildcard-first"}
        /\ candidates \in {<<1, 2>>, <<2, 1>>}
        /\ available \in SUBSET {1, 2}
        /\ badStage \in Stages
Next == UNCHANGED vars
Spec == Init /\ [][Next]_vars

OrderedEntriesSurvive ==
    \A entries \in {Build, Editor, Installed} : Identity(entries) = Identity(Authored)
NativeWinnerSurvives ==
    \A entries \in {Build, Editor, Installed}, query \in {"tie", "longer", "exact"} :
        Winner(entries, query) = Winner(Authored, query)
AppendedTiesCannotSteal ==
    EffectiveKeys # <<>> =>
        \A query \in {"tie", "longer", "exact"} :
            Winner(Editor \o <<Entry(5, "editor")>>, query) = Winner(Authored, query)

ScalarWinner(values, present) ==
    LET found == {i \in 1..Len(values) : values[i] \in present}
    IN IF found = {} THEN 0 ELSE values[First(found)]
FailedCandidates ==
    {candidates[i] : i \in {n \in 1..Len(candidates) :
        candidates[n] \notin available /\
        \A earlier \in 1..(n - 1) : candidates[earlier] \notin available}}
ScalarPublication(generated, stale, exact) ==
    LET selected == ScalarWinner(candidates, available)
        retained == IF exact /\ selected # 0 THEN <<selected>> ELSE candidates
        observed == IF PreserveFailedProbes THEN FailedCandidates ELSE {}
        conflict == observed \cap generated \cap Elements(retained) # {}
    IN IF conflict THEN [status |-> "conflict", selected |-> -1]
       ELSE [status |-> "published", selected |-> ScalarWinner(retained, available \cup stale)]
SuccessfulFallbackCannotReadKnownStaleScalar ==
    ScalarWinner(candidates, available) # 0 =>
        \A generated \in SUBSET {1, 2} :
            \A stale \in SUBSET (generated \ available), exact \in BOOLEAN :
                LET result == ScalarPublication(generated, stale, exact)
                IN (result.status = "conflict" /\ result.selected = -1) \/
                   (result.status = "published" /\ result.selected = ScalarWinner(candidates, available))
=============================================================================
