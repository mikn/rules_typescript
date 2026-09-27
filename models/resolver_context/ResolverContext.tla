------------------------- MODULE ResolverContext -------------------------
EXTENDS TLC

CONSTANTS Queries, Members, Mode
VARIABLES facts, phase, installed
vars == <<facts, phase, installed>>

ASSUME /\ Queries # {}
       /\ Members # {}
       /\ Mode \in {"Old", "Observed", "Retain", "AssumedNamespace", "RetainPlacement", "ContextFence"}

FactSpace == [known : BOOLEAN,
              coupled : BOOLEAN,
              artifactOutside : BOOLEAN,
              containment : {"excluded", "root", "unknown", "missing"},
              needsContainment : BOOLEAN,
              scope : {"module", "authored-paths", "generated-namespace"},
              materialized : BOOLEAN,
              members : [Members -> {"authored", "generated", "outside"}],
              manifestPathPreserved : BOOLEAN,
              packageMapping : {"absent", "first-party", "npm"},
              configBefore : BOOLEAN,
              configAfter : BOOLEAN,
              observed : (SUBSET Queries) \ {{}},
              dependent : SUBSET Queries,
              changedIdentity : {"other-source", "missing"}]

Consistent == /\ (facts.containment = "excluded" => ~facts.needsContainment)
              /\ (facts.containment = "root" => facts.needsContainment)
              /\ (facts.packageMapping = "npm" =>
                      ~facts.configBefore /\ ~facts.configAfter)

Previous == [root |-> "project", version |-> "previous"]
RequiredRoot == IF facts.artifactOutside /\ facts.needsContainment
                THEN "expanded" ELSE "project"
UnknownContainment == facts.containment = "unknown" \/
                      (facts.containment = "missing" /\ facts.artifactOutside)
GeneratedNamespaceCompatible ==
    facts.manifestPathPreserved /\
    (\A member \in Members : facts.members[member] # "authored")

ScopeConflict == ~facts.materialized \/ facts.scope = "authored-paths" \/
                 (facts.scope = "generated-namespace" /\ ~GeneratedNamespaceCompatible)
ContextConflict == (~facts.known \/ facts.coupled) /\ RequiredRoot # Previous.root
PlacementConflict == facts.coupled /\ facts.packageMapping = "first-party" /\
                     facts.configBefore # facts.configAfter

ResolveRoot(root, query) ==
    IF facts.coupled /\ root = "expanded" /\ query \in facts.dependent
    THEN facts.changedIdentity ELSE "source"

ResolveScope(version, query) ==
    IF version = "candidate" /\
       (~facts.materialized \/
        ((PlacementConflict \/ facts.scope = "authored-paths" \/
          (facts.scope = "generated-namespace" /\ ~GeneratedNamespaceCompatible)) /\
         query \in facts.dependent))
    THEN facts.changedIdentity ELSE "source"

Reject ==
    CASE Mode = "Observed" ->
             facts.coupled /\ facts.observed \cap facts.dependent # {}
      [] Mode = "AssumedNamespace" ->
             UnknownContainment \/ ~facts.materialized \/
             facts.scope = "authored-paths" \/ ContextConflict
      [] Mode = "RetainPlacement" ->
             UnknownContainment \/ ScopeConflict \/ ContextConflict
      [] Mode = "ContextFence" ->
             UnknownContainment \/ ScopeConflict \/ ContextConflict \/ PlacementConflict
      [] OTHER -> FALSE

Candidate == [root |-> CASE Mode = "Retain" -> Previous.root
                          [] Mode \in {"ContextFence", "AssumedNamespace", "RetainPlacement"} -> RequiredRoot
                          [] OTHER -> IF facts.artifactOutside THEN "expanded" ELSE "project",
              version |-> "candidate"]

Init == /\ facts \in FactSpace
        /\ Consistent
        /\ phase = "ready"
        /\ installed = Previous

PublishOrConflict ==
    /\ phase = "ready"
    /\ phase' = IF Reject THEN "conflict" ELSE "published"
    /\ installed' = IF Reject THEN installed ELSE Candidate
    /\ UNCHANGED facts

Done == /\ phase # "ready"
        /\ UNCHANGED vars

Next == PublishOrConflict \/ Done
Spec == Init /\ [][Next]_vars

LegacyContext == /\ facts.containment = "root"
                 /\ facts.known
                 /\ facts.scope = "module"
                 /\ facts.materialized

GeneratedContext == /\ facts.scope = "generated-namespace"
                    /\ facts.materialized
                    /\ facts.containment \in {"root", "excluded"}
                    /\ facts.known
                    /\ ~facts.coupled

PlacementContext == /\ facts.containment = "excluded"
                    /\ facts.known
                    /\ facts.coupled
                    /\ facts.scope = "module"
                    /\ facts.materialized
                    /\ facts.packageMapping = "first-party"
                    /\ ~facts.configBefore
                    /\ facts.configAfter
                    /\ facts.observed \cap facts.dependent = {}

TypeOK == /\ facts \in FactSpace
          /\ Consistent
          /\ phase \in {"ready", "conflict", "published"}
          /\ installed \in [root : {"project", "expanded"},
                             version : {"previous", "candidate"}]

PreservesSourceIdentity ==
    phase = "published" =>
        \A query \in Queries :
            ResolveRoot(installed.root, query) = ResolveRoot(Previous.root, query)

PreservesPackageContext ==
    phase = "published" =>
        \A query \in Queries :
            ResolveScope(installed.version, query) = ResolveScope(Previous.version, query)

ContainsGeneratedSources ==
    phase = "published" =>
        (RequiredRoot = "project" \/ installed.root = "expanded")

ConflictPreservesPrevious == phase = "conflict" => installed = Previous

SupportedContextPublishes ==
    phase # "ready" /\ ~UnknownContainment /\ ~ScopeConflict /\
    ~ContextConflict /\ ~PlacementConflict =>
        phase = "published" /\ installed.root = RequiredRoot

UnknownOutsideConflicts ==
    phase # "ready" /\ ~facts.known /\ RequiredRoot = "expanded" => phase = "conflict"

UnknownContainmentConflicts ==
    phase # "ready" /\ UnknownContainment => phase = "conflict"

IncompatibleNamespaceConflicts ==
    phase # "ready" /\ facts.scope = "generated-namespace" /\
    ~GeneratedNamespaceCompatible => phase = "conflict"

ChangedConfigPlacementConflicts ==
    phase # "ready" /\ PlacementConflict => phase = "conflict"
=============================================================================
