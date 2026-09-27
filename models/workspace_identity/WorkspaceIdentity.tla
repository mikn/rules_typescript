------------------------- MODULE WorkspaceIdentity -------------------------
EXTENDS Integers, Sequences, TLC

CONSTANT Mode
VARIABLES depth, source, coupled
vars == <<depth, source, coupled>>

ASSUME Mode \in {"Lexical", "Canonical"}

Workspace == <<"physical", "workspace">>
Spellings == {Workspace, <<"alias", "workspace">>}
SourceDirectories == {<<"physical">>, Workspace \o <<"generated">>, <<"outside">>}

CommonRoot(left, right) ==
    LET matches == {n \in 0..Len(left) :
                      n <= Len(right) /\ SubSeq(left, 1, n) = SubSeq(right, 1, n)}
        longest == CHOOSE n \in matches : \A m \in matches : n >= m
    IN SubSeq(left, 1, longest)

BoundWorkspace(spelling) == IF Mode = "Canonical" THEN Workspace ELSE spelling
Below(root, file) ==
    Len(root) <= Len(file) /\ SubSeq(file, 1, Len(root)) = root
CompilerSources == {<<"source.ts">>, <<"generated", "types.d.ts">>}
ExternalFiles == {<<"outside", "authored.d.ts">>, <<"physical", "workspace-other", "source.ts">>}
CompilerProjection(spelling, file) ==
    LET root == IF Below(spelling, file) THEN spelling ELSE BoundWorkspace(spelling)
    IN IF Below(root, file)
       THEN [scope |-> "workspace", file |-> SubSeq(file, Len(root) + 1, Len(file))]
       ELSE [scope |-> "external", file |-> file]
CompilerSourcesRetainProvenance ==
    \A spelling \in Spellings, relative \in CompilerSources :
        \A file \in {spelling \o relative, Workspace \o relative} :
            CompilerProjection(spelling, file) = [scope |-> "workspace", file |-> relative]
ExternalCompilerPathsStayExternal ==
    \A spelling \in Spellings, file \in ExternalFiles :
        CompilerProjection(spelling, file) = [scope |-> "external", file |-> file]

Root(spelling) == SubSeq(BoundWorkspace(spelling), 1, Len(Workspace) - depth)
RequiredRoot == CommonRoot(SubSeq(Workspace, 1, Len(Workspace) - depth), source)
ActualWidening == RequiredRoot # SubSeq(Workspace, 1, Len(Workspace) - depth)

Result(spelling) ==
    LET root == Root(spelling)
        candidate == CommonRoot(root, source)
    IN IF coupled /\ candidate # root
       THEN [status |-> "conflict", root |-> "previous"]
       ELSE [status |-> "published", root |-> IF coupled THEN candidate ELSE <<>>]

Init == /\ depth \in 0..Len(Workspace)
        /\ source \in SourceDirectories
        /\ coupled \in BOOLEAN

Next == UNCHANGED vars
Spec == Init /\ [][Next]_vars

AliasesHaveOneResult ==
    \A spelling \in Spellings : Result(spelling) = Result(Workspace)

SupportedContextPublishes ==
    (~coupled \/ ~ActualWidening) =>
        \A spelling \in Spellings :
            Result(spelling) = [status |-> "published", root |-> IF coupled THEN RequiredRoot ELSE <<>>]

ConflictRetainsPrevious ==
    (coupled /\ ActualWidening) =>
        \A spelling \in Spellings :
            Result(spelling) = [status |-> "conflict", root |-> "previous"]

=============================================================================
