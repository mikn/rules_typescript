-------------------------- MODULE CompilerRoots --------------------------
CONSTANTS CopyUnobserved, UsePatterns, RejectMissingRoots
VARIABLES compilerRoots, explicitRoots, missingGeneratedRoots
vars == <<compilerRoots, explicitRoots, missingGeneratedRoots>>

ASSUME /\ CopyUnobserved \in BOOLEAN
       /\ UsePatterns \in BOOLEAN
       /\ RejectMissingRoots \in BOOLEAN
Identities == {"literal", "sibling"}
Names == [root \in Identities |-> IF root = "literal" THEN "a?.ts" ELSE "ab.ts"]

Files == IF CopyUnobserved
         THEN compilerRoots \cup explicitRoots
         ELSE IF UsePatterns THEN compilerRoots \cap explicitRoots ELSE compilerRoots
Include == compilerRoots \ Files
SerializedFiles == {Names[root] : root \in Files}
SerializedInclude == {Names[root] : root \in Include}
LiteralMembership == {root \in Identities : Names[root] \in SerializedFiles}
PatternMembership == {root \in Identities :
    \E spec \in SerializedInclude : spec = Names[root] \/ spec = "a?.ts"}
EditorRoots == (LiteralMembership \cup PatternMembership) \ missingGeneratedRoots
Publish == ~RejectMissingRoots \/ missingGeneratedRoots = {}

Init == /\ compilerRoots \in SUBSET Identities
        /\ explicitRoots \in SUBSET Identities
        /\ missingGeneratedRoots \in SUBSET (explicitRoots \ compilerRoots)
Next == UNCHANGED vars
Spec == Init /\ [][Next]_vars

CompilerOwnsMembership == Publish => EditorRoots = compilerRoots
ObservedExplicitRootsStayExplicit ==
    Publish => compilerRoots \cap explicitRoots \subseteq LiteralMembership
MissingGeneratedRootsStayExplicit ==
    Publish => missingGeneratedRoots \subseteq LiteralMembership
=============================================================================
