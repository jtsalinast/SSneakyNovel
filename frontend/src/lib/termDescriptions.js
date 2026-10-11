// termDescriptions.js — round 26
// Exact-match second-line descriptions for EVERY term in the backend
// Conflict-scale and Protagonist-type catalogs (config.GenreConflictScales /
// GenreProtagonistTypes). The frontend looks terms up here first (keys are
// lowercase, matched via .trim().toLowerCase()); unmatched terms fall back to
// the generic keyword-family blurbs in GenreCascadeFields.svelte. This closes
// the gap where specific vocabulary ("Heavenly Tribulation", "Netrunner",
// "Dungeon Economy", …) previously rendered without any description.

export const CONFLICT_TERMS_EN = {
  'adaptation to new world rules':
    "The protagonist must decode and obey an unfamiliar world's physics, magic or social laws before those rules decide their fate.",
  'ancestral tech vs colonial remnant':
    'Ancient inherited technology clashes with the institutions left behind by colonizers — who legitimately owns the past?',
  'ancient politics':
    'Millennia-old powers play long games: alliances, grudges and protocols from forgotten eras still govern every move.',
  'ancient precursor threat':
    'A civilization that predates the current one stirs back to life, and its leftovers are hostile to everything now alive.',
  'audience vote manipulation':
    'Public voting or popularity systems become the battlefield — rigging, campaigning and fan wars decide real outcomes.',
  'body breaking before dream':
    'Physical limits threaten the ambition itself: the body fails before the talent does, forcing painful trade-offs.',
  'body count escalation':
    'Each kill or loss raises the stakes higher than the last, until the cost of continuing outweighs the goal.',
  'butterfly effect budget':
    'Every change to the timeline has a metered price — spend alterations wisely or pay compound interest.',
  'butterfly paradox fallout':
    'Small edits to events snowball into contradictions that threaten to unmake the present the protagonist is trying to save.',
  'campaign objective':
    'A military or political campaign imposes stepwise targets; each operation reshapes what the next one can afford.',
  'capture-target rivalry':
    'Two parties chase the same prize — the contest is less about the goal than about beating the rival to it.',
  'chain of command at sea':
    'Shipboard hierarchy: orders, mutiny risk and seamanship etiquette constrain every decision at sea.',
  'chain-of-command conflict':
    'Official orders and personal conscience pull in opposite directions inside a rigid rank structure.',
  'cheat ability with a cost':
    'An overpowered gift exists, but using it drains something precious — health, memory, time or bonds.',
  'child soldier chain of command':
    'Minors inherit adult roles in a war machine; competence collides with childhood and illegitimate authority.',
  'class divide above/below the deck':
    'The story runs on friction between the privileged upper decks and the exploited lower ones.',
  'class uprising':
    'An oppressed stratum rises against the order that feeds on them, from strikes to revolution.',
  'clock-running escape':
    "The breakout is a race against a fixed timer — every corridor costs seconds the plot won't refund.",
  'collateral damage spiral':
    'Each victory creates new victims whose grievances fuel the next conflict.',
  'colony vs earth politics':
    'A space colony demands autonomy while the home planet insists on control and tribute.',
  'con-artist vs mark of the week':
    "Episodic grift: each target is a new job, and the scheme of the week tests the con artist's craft.",
  'contract fine print':
    "The deal's hidden clauses become the antagonist — literalism, loopholes and obligations negotiated in blood or ink.",
  'cooperative governance':
    'Collectives argue themselves into decisions: consensus, votes and shared ownership under strain.',
  'core corruption risk':
    'A power core — magical or technological — degrades or turns malicious with each use.',
  'corp vs street':
    'Megacorporate machinery grinds against street-level lives; survival means outrunning both.',
  'corrupted mentor revelation':
    'The teacher who guided the hero is revealed to have served another cause all along.',
  'dao vs demon path':
    'Cultivation choice between disciplined righteousness and shortcut power that devours the self.',
  'demon lord countdown':
    'A sealed evil has a release date; every arc buys or loses time before it wakes.',
  'diaspora homecoming claim':
    'Scattered people return to claim a homeland, inheritance or throne they were exiled from.',
  'dignity vs fan-service gravity':
    'The character fights to be taken seriously in a world that keeps reducing them to spectacle.',
  'double-cross':
    'Alliances exist only to be betrayed; the tension is when and by whom.',
  'dungeon economy':
    'Dungeons as resource systems: loot supply, guild pricing and monster farming shape every decision.',
  'ecological balance':
    'Interventions in an ecosystem rebound on the interveners — protection vs exploitation.',
  'escape or reform':
    'Cornered characters choose between running away and fixing the broken system from within.',
  'etiquette minefield':
    'High-society rules of conduct: one wrong curtsy or phrase detonates reputations.',
  'exam hierarchy pressure':
    'Ranking exams and academic tiers decide worth, futures and friendships in a school system.',
  'fate vs defiance':
    'Prophecy or destiny scripts the ending; the whole struggle is refusing the written role.',
  'federation vs separatists':
    'A union fractures: member worlds or regions fight to leave, and loyalty splits down the middle.',
  'festival arc stakes':
    'A tournament or celebration compresses rivalries and revelations into a few spotlight days.',
  'first contact protocol failure':
    'Humanity meets an alien intelligence with the wrong script — diplomacy collapses into misunderstanding.',
  'flag misreading comedy':
    'Romantic signals are read completely wrong by everyone involved; the farce escalates from politeness.',
  'floor ecology balance':
    'Tower/dungeon floors each host fragile ecosystems that break if adventurers harvest too greedily.',
  'forbidden kinship':
    'Love or loyalty across blood ties, sect lines or castes that the world forbids outright.',
  'forced alliance politics':
    'Enemies must co-govern or co-fight; every meeting is a negotiation nobody trusts.',
  'friendship group realignment':
    "A friend circle's internal ranking shifts — new loyalties dethrone old ones.",
  'generation-ship politics':
    'On a ship where nobody will see the destination, governance, belief and revolt drift apart by deck and caste.',
  'global politics':
    'Nation-states, treaties and world opinion weigh in on every incident.',
  'good vs evil':
    'Classic moral polarity made operational: armies, temptations and choices framed as light against dark.',
  'heart vs scripted ending':
    'In a game/otome world, genuine feeling rebels against the route the script says you must follow.',
  'heavenly tribulation':
    'Cosmic lightning trials punish cultivation breakthroughs — power literally costs being struck by heaven.',
  'hero incursions vs core growth':
    'A dungeon/monster core develops quietly while heroes keep raiding it, threatening its evolution.',
  'hero vs arch-nemesis cycle':
    'The recurring duel defines both careers; neither can win without the other.',
  'hidden society exposure':
    'A secret community risks discovery by the ordinary world — containment vs disclosure.',
  'holiday deadline':
    'Festive calendar pressure: everything must resolve before the season ends.',
  'immortal realm power struggle':
    'Immortals compete for daoist thrones, resources and heavenly bureaucracy posts that never expire.',
  'inner conflict made visible':
    "Psychological wounds manifest externally — doubles, monsters or curses embodying the character's pain.",
  'institution vs student':
    'The school/guild apparatus protects itself at the expense of the individual it claims to serve.',
  'institutional corruption':
    'Rot inside organizations: whistleblowers, cover-ups and careers built on buried files.',
  'institutional wall':
    'Change blocked not by villains but by procedure, indifference and immovable bureaucracy.',
  'insurgency & occupation':
    'An occupying force holds territory while a resistance wins hearts, bombs or neither.',
  'intellectual rivalry turned dangerous':
    'Two minds competing over ideas escalate into sabotage, obsession or violence.',
  interstellar:
    'Voyages between stars: distances, relativity and isolation pressurize crew and mission alike.',
  'jealousy without villainy':
    'No antagonist needed — envy and insecurity among good people create the friction.',
  'jianghu feud':
    'Martial-world grudges between sects and wanderers, governed by honor, face and old debts.',
  'known disaster deadline':
    'Everyone knows when the catastrophe hits; the drama is whether preparations finish first.',
  'law vs lawlessness':
    'Frontier or noir justice: statutes, sheriffs and vigilantes disagree about what order means.',
  'legacy hero vs new generation':
    "The famous predecessor's shadow forces the successor to surpass or escape them.",
  'legacy of collapse':
    'Characters inherit the wreckage of a fallen civilization and live with its failure modes.',
  'lie snowballing out of control':
    'One deception requires two more; comedy and dread scale until confession is the only exit.',
  'local crime':
    'Neighborhood-scale offenses: petty thefts, gangs and small-time deals with personal consequences.',
  'local politics knowledge gap':
    "A newcomer misses the town's unwritten history — every conversation hides landmines only locals hear.",
  'magic society audit':
    'Bureaucratic scrutiny of magical practice: licensing, regulation and the politics of who may cast.',
  'man vs sea & ship mechanics':
    'Wind, waves and faulty rigging are the antagonists; competence against ocean is the plot.',
  'mana exhaustion real cost':
    'Spellcasting burns finite inner resources; overuse means collapse, debt or worse.',
  'martial supremacy':
    'Sects and empires settle hierarchy through combat prowess — losing means losing everything.',
  'memory vs propaganda':
    'Personal recollection battles the official story; whoever controls archives controls truth.',
  'migration under heat':
    'Climate or persecution forces movement; the journey itself is the obstacle course.',
  'misreading every signal':
    'Social perception is entirely broken — every gesture interpreted backwards, escalating chaos.',
  'monster-of-the-week escalation':
    'Serial threats arrive one by one, each stronger, until the pattern itself becomes the mystery.',
  'moral line defined early':
    'The protagonist set their rule at the start; the plot tests whether they will break it.',
  'mutiny brewing below decks':
    "Crew resentment accumulates toward the moment the captain's authority is formally challenged.",
  'neighbor misunderstanding':
    'Domestic proximity conflicts: noise, fences, gossip — small frictions that fester.',
  'oblivious protagonist consequences':
    "The lead can't see what their actions do to others; the world keeps the score instead.",
  'occult industry accident':
    'Paranormal services go wrong on the job — cursed products, botched exorcisms, supernatural liability.',
  'old-world remnant threat':
    'Leftovers of the pre-apocalypse era resurface as danger: machines, pathogens or ideologies.',
  'one last job':
    'Retired specialists take a final score; the plan works until the person does.',
  'ordinary life vs calling':
    'Comfortable anonymity competes with a summons to adventure, service or greatness.',
  'past crime surfacing in the present':
    'An old offense crawls back into a rebuilt life through evidence, blackmail or coincidence.',
  'past trauma':
    'Unprocessed wounds replay themselves, distorting judgment until confronted.',
  'patron demands vs freedom':
    'An artist or knight funded by a powerful backer trades autonomy for support.',
  'physics wins vs human hope':
    'Hard science constraints defeat optimism; the drama is finding meaning anyway.',
  'pilot sync cost':
    'Mechs demand neural compatibility; syncing taxes identity, health or years of life.',
  'pirate confederacy':
    'Rogue captains attempt collective government — democracy with cannons.',
  planetary:
    'Whole-planet scope: biospheres, weather systems or global governments as the stage and stake.',
  'precinct chain-of-casework':
    'Police procedure as ladder: paperwork and assigned cases grind the pursuit of the big one.',
  'pride vs attraction':
    'Both leads would rather lose than admit they want each other.',
  'production line logistics':
    'Mass output under deadlines: bottlenecks, quotas and supply chains decide the war or business.',
  'prophecy bond':
    'A prediction ties two fates together — usually one destined to destroy the other.',
  'psychological terror':
    'The threat is mental: gaslighting, paranoia and dread without needing a physical monster.',
  'public trust after collateral damage':
    'After a costly rescue the crowd turns; rebuilding legitimacy becomes the mission.',
  'race against time':
    'A ticking clock governs the entire plot structure.',
  'race to repair a fixed point':
    "History's key moments must be restored before tampering unravels the present.",
  'reason vs encroaching madness':
    'Sanity erodes literally or socially; the fight is staying coherent in an irrational world.',
  'religious conflicts':
    'Dogma, schism and faith-run institutions clash over souls, land or interpretation.',
  'reputation at stake':
    'Honor economies: what people believe about you matters more than what you did.',
  'resistance cell':
    'Small clandestine groups operate under occupation, trusting no one outside the unit.',
  'return home vs save this one':
    'The way out and the person who needs you point in opposite directions.',
  'revenge chain':
    'Vengeance begets vengeance; each act recruits the next avenger.',
  'revenge on the high seas':
    'Pirate-era vendettas fought ship-to-ship across years and oceans.',
  'rewilding vs industry':
    'Restoring wilderness collides with jobs, infrastructure and settled ways of life.',
  "rewriting the heroine's story":
    "A side character or reader-insert takes pen to the original heroine's doomed script.",
  'righteous vs unorthodox paths':
    'Two legitimate ways to power — orthodox discipline vs heterodox shortcuts — in competition.',
  'rival generation handoff':
    "Veterans must pass the torch to rivals-in-waiting who aren't ready, or don't want it.",
  'rival tease escalation':
    'Friendly antagonism expressed through escalating pranks and provocations.',
  'route-lock escape':
    'Trapped inside a predetermined romance/game route, the character fights the rails themselves.',
  'running one step ahead of the law':
    'Cat-and-mouse survival: the fugitive stays free by always moving first.',
  'scripted doom counterplan':
    "Knowing the apocalypse's exact schedule, the protagonist builds countermeasures beat by beat.",
  'season change forcing adaptation':
    'Weather and seasonal cycles impose hard deadlines on travel, harvest or survival.',
  'selection cutline pressure':
    'Idol/sports trainings live under periodic elimination rankings.',
  'serial pattern vs bureau politics':
    "A case's repeating pattern exposes inter-agency rivalry more than the criminal.",
  'serious mission absurd interruptions':
    'High-stakes operations constantly derailed by comedic incompetence or circumstance.',
  'settlement vs raiders':
    'A frontier community fortifies, negotiates or fights off recurrent plunderers.',
  'shared goal bonds':
    'A joint objective slowly welds mismatched people together.',
  'shrinking arena pressure':
    'Battle royale: the safe zone contracts and forces confrontation.',
  'small daily frictions':
    'Slice-of-life micro-conflicts: chores, habits, mild annoyances that accumulate meaning.',
  'society rules vs rogue ethics':
    'Outlaws with codes collide with lawful societies without them.',
  'someone actually has stakes':
    'Everyone else treats the game casually except one player who desperately needs to win.',
  'spellcraft ethics vs raw talent':
    'Gifted sorcery without scruples versus disciplined practice with limits.',
  'spirit network uprising':
    'Connected spiritual infrastructure (leylines, ghosts, servers) organizes against its users.',
  'spy networks':
    'Handlers, assets and dead drops — intelligence machinery as the battlefield.',
  'summit myth economics':
    'Pilgrims pay to climb a mountain whose promised miracle is disputed by those who run the route.',
  'supernatural threat':
    'Ghosts, demons and anomalies beyond conventional weapons or law enforcement.',
  "survivor's guilt math":
    'Counting who deserved to live and who did not, and paying that ledger.',
  'talent vs discipline':
    'Natural gifts square off against relentless training.',
  'teacher administration friction':
    'Classroom reality versus administrative mandates and politics.',
  'terraform ethics':
    'Remaking a planet raises consent questions from indigenous life, settlers and future generations.',
  'ticking clock rescue':
    'A hostage or bomb sets the rhythm; every subplot measured against it.',
  'time & miscommunication farce':
    'Mistimed letters and missed cues generate the whole comedy.',
  'trust nobody game':
    'Free-for-all suspicion: information is the weapon and alliances are temporary bids.',
  'uncharted frontier':
    'Unknown territory itself is the antagonist: maps end, resources thin, rules absent.',
  'witch-knight rivalry':
    'Magic users and armored orders contest status, jurisdiction and ideology.',
  'xeno diplomacy':
    'Negotiating with genuinely alien minds, values and biology.',

  'ai emancipation':
    'A created intelligence fights to be recognized as a person with rights rather than property to be switched off.',
  'adventurer guild pressure':
    'The Guild applies contract, rank and fee pressure on freelancers — bureaucracy as antagonist for active adventurers.',
  'allies gathered one debt at a time':
    'Support comes from favors owed, not friendship; every ally is another obligation ledger entry.',
  'ancient evil':
    'A primordial malevolent force wakes on its own schedule, indifferent to the politics of the present.',
  'ancient trap/trial':
    'Dead civilizations left lethal tests still running; solving them is archaeology under fire.',
  'boss rush final showdown':
    'Consecutive overwhelming enemies with no recovery between — endurance against scripted peaks.',
  'boundaries negotiated late':
    'The relationship only defines its rules after lines were already crossed; repair over foundation.',
  'broken engagement fallout':
    'A public engagement collapses, scattering obligations, alliances and gossip through the social web.',
  'career crossroads vs love':
    'A promotion or calling in one city competes with a relationship anchored in another.',
  'career vs love':
    'Professional ascent and romantic commitment demand the same hours and the same loyalty.',
  'checkpoint death costs':
    'Dying resets progress and charges resources; mortality has a price tag that shapes decisions.',
  'city-wide supernatural war':
    'Hidden powers fight openly across an entire urban grid, with civilians as terrain.',
  'civilization building':
    'Founding institutions from scratch — law, agriculture, defense — against entropy and neighbors.',
  'civilization vs wilderness':
    'Settlement expansion meets an ecosystem or culture that refuses to be cleared.',
  'climber guild cartel':
    'Tower-climbing guilds fix prices and routes; independent climbers face organized gatekeeping.',
  'club politics':
    'A school or hobby club becomes a constitutional crisis: officers, factions and bylaws.',
  'cold case reopened':
    'A decades-old investigation restarts, usually because new evidence implicates someone powerful.',
  'community event deadline':
    'A festival, opening or funeral sets the clock the whole neighborhood organizes around.',
  'community resilience':
    'A group absorbs shocks — floods, closures, losses — testing what holds them together.',
  'community secret':
    'Everyone in town knows the thing; the story is what happens when outsiders arrive.',
  'community story':
    'Ensemble narrative where the community itself, not any individual, drives the plot.',
  'community vs utility corporation':
    'Residents battle the provider that owns their water, power or data — meters as weapons.',
  'cosmic indifference':
    'The universe does not care whether humanity survives; meaning must be manufactured locally.',
  'court gossip economy':
    'Rumor functions as currency and weapon inside aristocratic circles.',
  'court vs jianghu':
    'Imperial law collides with martial-world codes — two jurisdictions claiming the same sword.',
  'crew loyalty vs captain\'s orders':
    'The crew loves their captain precisely enough to question the order they cannot follow.',
  'daily survival':
    'No grand quest — today\'s food, warmth and safety are the entire plot, repeated and varied.',
  'data heist':
    'Stealing information rather than objects: intrusion, exfiltration and the trace left behind.',
  'doom vs devotion':
    'The apocalypse makes vows urgent; commitment is tested against an expiring world.',
  'drought resource wars':
    'Water shortage converts neighbors, farms and cities into competitors.',
  'duelist etiquette wars':
    'Formal combat culture where insult, seconds and protocol escalate as fast as steel.',
  'duty vs desire':
    'Assigned obligation conflicts with personally chosen longing.',
  'dying school inheritance':
    'The last master of a fading discipline decides who — if anyone — deserves the lineage.',
  'ego vs reality check':
    'A protagonist\'s inflated self-image meets consequences that refuse to flatter it.',
  'empire machinery vs individual':
    'Bureaucratic imperial logistics grind personal lives into administrative units.',
  'empire vs free companies':
    'Centralized state power versus mercantile/mercenary franchises answering to profit.',
  'enemy-to-lover duty':
    'Hostile agents forced into proximity by duty discover the other side of the uniform.',
  'engagement deadline':
    'A marriage contract carries an expiry date; love must be produced on schedule.',
  'escalating prank or bet':
    'A joke wager escalates past cruelty or risk because nobody will fold first.',
  'evidence chain over years':
    'Justice pursued patiently: documents, witnesses and forensics assembled across decades.',
  'expedition rivalry':
    'Two teams race up the same mountain, trench or dig — sportsmanship under altitude.',
  'fake relationship turns real':
    'A convenience arrangement develops actual feelings neither party budgeted for.',
  'family curse':
    'An inherited affliction replays each generation unless someone breaks the pattern.',
  'family issues':
    'Ordinary dysfunction — money, addiction, favoritism — dramatized without melodrama.',
  'family mystery':
    'A puzzle embedded in kinship: adoptions, disappearances, sealed rooms, wrong names.',
  'fate reasserting itself':
    'Escaped destinies hunt down those who dodged them; the timeline corrects like gravity.',
  'floor-by-floor trial ladder':
    'Progress measured in levels: each floor a distinct ordeal with escalating logic.',
  'forbidden spell temptation':
    'Power everyone agreed to bury whispers specifically to its most disciplined scholar.',
  'galactic':
    'Scale term: politics, travel and warfare spanning thousands of star systems.',
  'galactic empire succession':
    'Interstellar throne transitions trigger civil wars fought across light-years.',
  'gatekeeper boss contract':
    'A literal guardian of a threshold negotiates tolls, trials or alliances before passage.',
  'generational legacy':
    'Choices made by grandparents decide opportunities their grandchildren inherit.',
  'government secrets':
    'Classified truth presses against citizens\' right to know — whistleblowers and archives.',
  'guild vs inventor monopoly':
    'Craft guilds suppress disruptive inventors whose machines threaten the ordering of labor.',
  'historical puzzle':
    'A mystery legible only through period sources: codes, maps and dead languages.',
  'history police vs personal grief':
    'Official memory management clashes with a family mourning outside approved narrative.',
  'house vs family memory':
    'Ancestral home as contested archive — renovation versus remembrance.',
  'humanity vs technology':
    'The species renegotiates what remains human as its tools become autonomous.',
  'identity unraveling':
    'A stable self-concept loses coherence — amnesia, doubles, or discovered lies.',
  'identity vs augmentation':
    'When bodies can be rebuilt, which parts count as the person?',
  'individual vs the regime':
    'One private life standing against a political system that requires conformity.',
  'insider knowledge temptation':
    'Foreknowledge — market, magical or temporal — invites profitable corruption.',
  'institution vs individual':
    'Organizational self-preservation versus a single member\'s needs or conscience.',
  'international conspiracy':
    'Multi-government shadow coordination exposed by whoever was never supposed to see it.',
  'kingdom-wide':
    'Scope term: the conflict engulfs an entire realm\'s provinces, courts and roads.',
  'loot economy wars':
    'Reward distribution mechanics become the battlefield — who owns drops, who profits.',
  'love across eras':
    'Romance separated or enabled by time displacement: letters across centuries.',
  'magical court intrigue':
    'Sorcerous aristocracy scheming through ritual, favor and enchanted law.',
  'man vs nature':
    'Elemental antagonism without villains: weather, terrain and biology against intention.',
  'memory vs truth':
    'What happened versus what is remembered — and which one governs the reconciliation.',
  'mirror-of-self target':
    'The adversary is a reflection: same origin, opposite choices.',
  'national security':
    'State protection imperatives that justify secrecy, surveillance and sacrificed liberties.',
  'neighborhood turf':
    'Micro-territorial conflict: blocks, corners and beats defended as sovereign ground.',
  'old debt collecting in blood':
    'A settled-looking obligation resurfaces with interest payable in violence.',
  'patron debt':
    'Owed gratitude or money to a benefactor whose requests keep crossing ethical lines.',
  'personal':
    'Scope term: stakes confined to one individual\'s life, relationships or vocation.',
  'personal growth':
    'The contest is internal: competence, confidence and self-knowledge compound slowly.',
  'personal haunting':
    'One specific unresolved loss or guilt behaves like a ghost attached to a person.',
  'personal mystery':
    'An unknown about oneself — gaps in memory, inherited secrets, unexplained talent.',
  'personal quest':
    'A mission defined by private need rather than prophecy, payment or politics.',
  'personal vendetta':
    'Grudge carried individually; the entire plot is one account between two people.',
  'personal vs visual legacy':
    'Private memory of a person versus their public image and iconography.',
  'political alliance vs heart':
    'Dynastic or factional bargaining demands a marriage the heart refuses.',
  'political intrigue':
    'Competing factions maneuver through office, rumor and patronage short of open war.',
  'power corruption temptation':
    'Acquired strength quietly rewrites the moral reasoning of its holder.',
  'quiet personal milestone':
    'Achievement without audience: the first honest step counted only by the self.',
  'range war':
    'Land-use conflict — ranchers, farmers, fences and water — played out at gunpoint.',
  'rebuilding vs warlordism':
    'Post-collapse reconstruction constantly nearly captured by local armed strongmen.',
  'relationship negotiation rules':
    'Couples explicitly drafting how they argue, commit and separate — romance as treaty work.',
  'relationship obstacles':
    'External blockers: distance, class, family, timing — love against logistics.',
  'resource scarcity':
    'Shortage of essentials structuring every trade, migration and skirmish.',
  'responsibility vs personal life':
    'Caregiving, command or debt consuming the private self it nominally protects.',
  'rival crews collide':
    'Two competent teams on intersecting paths: jobs, territory or hearts overlap.',
  'rival-to-lover escalation':
    'Antagonism intensifies until both admit the obsession was attraction wearing armor.',
  'scarcity ethics':
    'Who deserves the last ration, dose or seat — triage as moral drama.',
  'second-chance relationships':
    'Old lovers reunite with changed selves and unfinished grammar.',
  'secret identity vs friendship':
    'The masquerade protects the mission but poisons the one genuine bond available.',
  'secret society initiation cost':
    'Admission priced in severed ties, oaths or body — worth it only after payment.',
  'sect competition':
    'Parallel schools vie for disciples, resources and doctrinal supremacy.',
  'sect honor':
    'Institutional face: individual shame endangers the whole school\'s standing.',
  'sect/realm wars':
    'Organized cultivation orders wage territorial war with metaphysical weaponry.',
  'self vs meaning':
    'Identity confronting purposelessness: existing well without a reason why.',
  'server-wide meta war':
    'MMO economies and faction politics escalated to whole-shard consequence.',
  'silence inside a family':
    'Unspoken agreements as atmosphere; everyone protecting everyone from the topic.',
  'small-town gossip':
    'Information traveling by whisper: reputations inflate and distort block by block.',
  'social awkwardness cascade':
    'One misread cue compounds through politeness, panic and performance.',
  'solo vs guild':
    'Independent operator refusing membership pays in access, protection and prices.',
  'storm or wreck survival':
    'Single catastrophic event at sea or mountain: immediate triage, then aftermath.',
  'survival struggles':
    'Sustained hardship against environment, poverty or injury without heroic framing.',
  'survival in the unknown':
    'Displacement into unmapped conditions where prior experience stops applying.',
  'system rules vs free will':
    'Game-like cosmic governance versus stubborn human refusal to be scripted.',
  'team ego vs common goal':
    'Star personalities make the team lose by being right individually.',
  'the score vs the crew':
    'Heist tension between taking the money and keeping the people.',
  'timing & miscommunication farce':
    'Mistimed arrivals and undelivered messages generate the entire comedy.',
  'town protection':
    'A settlement choosing defense, tribute or evacuation under recurrent threat.',
  'treasure that tears a crew apart':
    'Shared prize converting partners into adversaries over division.',
  'tribal warfare':
    'Inter-group conflict governed by kinship, raiding tradition and older grudges.',
  'tutorial to raid boss':
    'Progression arc compressed from first lessons to apex encounter.',
  'unit survival':
    'A small military or work unit sustaining cohesion under attrition.',
  'utopia defending itself':
    'The perfect society discovers the maintenance workload of perfection.',
  'victim community vs procedure':
    'Affected people pressing claims against institutional process and statute of limits.',
  'war broadcast propaganda':
    'Media apparatus shaping a conflict while individual journalists defect from script.',
  'war vs conscience':
    'Personal morality resisting legitimate military necessity.',
  'water rights court battle':
    'Litigation over rivers and aquifers where hydrology becomes testimony.',
  'world-saving':
    'Existential stakes: preventing extinction, invasion or unraveling of reality itself.',
};
export const PROTAGONIST_TERMS_EN = {
  'magic user':
    "Works the world's supernatural system directly — spells, mana or contracts — and answers for the consequences.",
  'knight/warrior':
    'Sworn blade: duty, code and martial skill define them more than birth or talent.',
  'common hero':
    'No prophecy, no cheat item — an ordinary person doing heroic things anyway.',
  diplomat:
    'Fights with language, treaties and banquets; wars averted or started by a sentence.',
  'shop owner':
    'Runs a business at the center of town — inventory, regulars and secrets behind the counter.',
  'local resident':
    'Deeply rooted in one place; the story happens to their community and they answer for it.',
  'hobby enthusiast':
    'Amateur passion (photography, trains, cooking) becomes unexpectedly load-bearing skill.',
  'secret agent':
    'Covert operative whose real loyalties and name are both classified.',
  'double agent':
    'Serves two masters, betraying one of them — the suspense is which.',
  'spy handler':
    "Manages assets in the field; their weapon is other people's trust.",
  'undercover operative':
    'Living a false identity so long the real one starts to fade.',
  'government analyst':
    'Sees the whole board from a desk — and can barely move any piece.',
  'single parent':
    'Guardian of kids with no backup; every risk is multiplied by responsibility.',
  'artist/creative':
    'Makes things that did not need making; the work is how they process the world.',
  'business owner':
    'Entrepreneur carrying payroll, reputation and ambition in the same ledger.',
  'healthcare worker':
    'Nurse, medic or doctor: care labor under shortage, trauma and moral triage.',
  'haunted individual':
    'Literally or figuratively pursued by spirits/pasts; sensitivity is both gift and wound.',
  'gothic hero':
    'Protagonist shaped by decay, ancestral curses and brooding ruins — drawn to what undoes them.',
  'tormented soul':
    'Interior anguish externalized by the plot: each event presses exactly on their wound.',
  'ancient warrior':
    'A fighter from a fallen era wakes or endures in a world that forgot why they fought.',
  'priest/priestess':
    'Ordained servant of a faith whose gods may or may not answer back.',
  slave:
    'Property seeking personhood and freedom within systems designed to deny both.',
  'ancient ruler':
    'Sovereign of a lost empire — legitimacy, ritual and command survive their own state.',
  'sheriff/marshal':
    'The law at the edge of the map: badge, boundaries and not enough deputies.',
  gunslinger:
    'Fast draw, slower conscience; hired for violence and judged by it.',
  rancher:
    'Steward of land and herd against drought, fence wars and developers.',
  outlaw:
    'Outside the law by choice or circumstance; the gang is family and liability.',
  'bounty hunter':
    'Takes payment for capture; every contract tests where the line is.',
  'game designer trapped inside':
    'Made the rules, now must survive them — meta-knowledge is the only cheat.',
  'support class strategist':
    'Plays the buff/healer role but wins with tactical vision others lack.',
  speedrunner:
    'Optimizes routes and skips content — treating the world as a sequence to beat.',
  'npc who wakes up':
    'Background character gains self-awareness inside a scripted world.',
  'blue-collar with a secret':
    'Trades, uniforms and calluses hiding an unusual second life.',
  'building manager':
    'Keeps a building alive: tenants, pipes, budgets and the stories in unit 4B.',
  journalist:
    'Chases the story as public service — sources, editors and danger in that order.',
  'fixer for hidden folk':
    'Broker between the mundane world and magical beings who need paperwork done.',
  'sergeant nco':
    'The spine of the platoon: orders translated into survivors.',
  'sniper/scout':
    'Eyes and long-range teeth; patience, distance and the ethics of the unseen shot.',
  'conscript turned veteran':
    'Pressed into service, hardened into expertise — fighting for reasons they once rejected.',
  scavenger:
    "Salvages the old world's corpse for parts, food and trouble.",
  'young-born (never saw the old world)':
    "Raised entirely in the post-collapse or artificial world; its myths are someone else's memory.",
  'mountain guide':
    'Leads others through lethal terrain for pay; the mountain decides everything.',
  'treasure hunter with debts':
    'Relic hunting as loan repayment — the treasure always costs more than it pays.',
  'documentary filmmaker':
    'Camera as protagonist: truth, framing and consent while history happens on record.',
  'disciple of a fallen sect':
    'Last student of a destroyed tradition preserving or purging its legacy.',
  'wandering swordsman':
    "Masterless blade moving town to town, settling debts that aren't theirs.",
  'court constable in jianghu':
    'Official lawman operating inside martial-world codes that outrank statutes.',
  'alchemist apprentice':
    'Learning transmutation under a master — recipes, ingredients and forbidden experiments.',
  'demon-cultivator defying fate':
    'Practices the forbidden path precisely because heaven wrote them off.',
  'body cultivator with broken meridians':
    'Cannot use inner energy, so forged the flesh itself into the weapon.',
  'bookshop owner':
    'Keeper of a shop and its regulars; books connect (and occasionally endanger) lives.',
  gardener:
    'Tends growing things; horticulture becomes the lens for patience, seasons and grief.',
  'newcomer in a small town':
    "The stranger whose arrival rearranges the village's equilibrium.",
  'tech forger':
    "Builds or falsifies hardware/documents for people who can't ask the real authorities.",
  'getaway driver':
    'Professional escape artist; the heist lives or dies in ninety seconds of driving.',
  'inside man':
    'Recruited accomplice already positioned when the plan begins.',
  'crew assembler':
    'Talent scout for jobs: picks the specialist for each phase of the score.',
  'street samurai':
    'Cyberpunk sellsword: blade, augmentation and a personal code in a corporate jungle.',
  ripperdoc:
    'Augmentation surgeon for the underserved — chrome, ethics and back-alley prices.',
  netrunner:
    'Dives corporate nets for data; the body waits in a chair somewhere.',
  'corp defector':
    'Insider who walked out of the megacorp with knowledge they want buried.',
  fixer:
    'Information broker and deal-maker connecting mercs to employers.',
  'data courier':
    "Carries sensitive payloads physically because networks can't be trusted.",
  rewildler:
    'Practitioner of ecological restoration, fighting for species and corridors.',
  'cooperative organizer':
    'Builds member-owned enterprises against incumbent extraction.',
  'rooftop farmer':
    'Grows food above a hungry city — small plots, large symbolism.',
  'restoration architect':
    'Rebuilds ruined heritage buildings, arguing authenticity against budget.',
  'mortal envoy to fae courts':
    'Human diplomat navigating immortal etiquette where words are binding traps.',
  'god-touched general':
    'Commander marked by divinity — tactics blessed, autonomy questionable.',
  'witch bound by oath':
    'Power conditional on vows; every episode probes the wording.',
  'reluctant messenger':
    'Carries messages or destinies they never asked to hold.',
  'archive keeper':
    'Guards institutional memory; access requests reveal what power wants forgotten.',
  'district laborer':
    'Workhorse of an industrial quarter; solidarity and scarcity frame the day.',
  'elite student tasting truth':
    'Top-tier academy pupil discovers the curriculum hides something.',
  'smuggler of banned books':
    'Moves prohibited reading past censors, one crate at a time.',
  'disgraced admiral':
    'Fleet commander stripped of honor, awaiting vindication or exile.',
  xenolinguist:
    'Decodes alien languages where grammar itself is the frontier.',
  'marine on a doomed colony ship':
    'Soldier protecting settlers whose vessel was never meant to arrive.',
  'silent observer':
    'Speaks little, sees everything; narration withheld until it matters.',
  'visual thinker misread by others':
    'Processes the world spatially; people mistake difference for deficit.',
  'archivist of family pictures':
    "Sorts other people's photographs — and inherits their unresolved pasts.",
  'street kid sketching the city':
    'Homeless artist whose drawings witness what adults walk past.',
  'charming privateer':
    'Legal-ish pirate with a smile; letters of marque and loose interpretation.',
  'boatswain keeping crew honest':
    'The foreman of the deck: standards, grumbling and unspoken loyalty.',
  'stowaway with useful hands':
    'Hitched aboard unnoticed, then proves indispensable at the worst time.',
  'lighthouse keeper drawn into a wreck':
    'Solitary guardian pulled into the aftermath of a disaster under their beam.',
  'cabin passenger hiding a past':
    "Respectable traveler whose ticket name isn't the true one.",
  'deckhand learning the rope code':
    'Initiate absorbing maritime hierarchy knot by knot.',
  'wandering actor collecting stories':
    "Traveling performer trading tales; other people's lives become repertoire.",
  'live-in governess or tutor':
    "Educator inside the household — intimacy, boundaries and the family's secrets.",
  'last scion returning for a funeral':
    'Heir comes home to bury the patriarch and reopen everything.',
  'architect restoring the ruined house':
    "Renovation narrative: the building's history rewrites the renovator.",
  'tenured professor with something buried':
    'Career security covering a younger mistake that resurfaces.',
  'archivist or rare-books curator':
    'Specialist custodian whose collection attracts forgery, theft and obsession.',
  'initiate who questions the society':
    'New member of a fraternal order spotting its contradictions early.',
  'roommate nobody believes':
    'Witness to strangeness living with skeptics — credibility as the fight.',
  'clockwork apprentice':
    'Trainee engineer of automata in a gear-driven world; precision culture.',
  'investigative journalist with gadgetry':
    'Reporter whose hobbyist tech turns scoops into surveillance thrillers.',
  'union organizer in the mills':
    'Builds collective action against mill owners, strike by strike.',
  'otherworldly medium hired by industry':
    'Spirit channel contracted by corporations — seance as service.',
  'accidental tourist through eras':
    'Time traveler with no mission, only displacement and wonder.',
  'temporal repair technician':
    'Fixes scheduled history like plumbing; overtime is causality.',
  'griever hunting one fixed moment':
    'Specialized agent pursuing whoever altered a single canonical event.',
  'historian living inside sources':
    'Lives so deep in primary records that the past argues with the present.',
  'smuggler of small knowledge':
    'Carries trivial-looking facts that turn out to be keys.',
  'water rights lawyer':
    'Litigates who may drink — aquifers, treaties and farms in court.',
  'migration community organizer':
    "Coordinates displaced families' claims against borders and bureaucracy.",
  'ancestral tech inheritor':
    'Receives dormant family machines only they can activate — and everyone wants.',
  'spirit-network hacker':
    'Breaches the ghost/web hybrid infrastructure communing with its residues.',
  'utopian guard with doubts':
    "Protects a perfect society whose flaws they've personally logged.",
  'griot preserving encoded history':
    'Oral historian whose songs hide data the powerful want lost.',
  'generation-ship mediator':
    'Arbitrates castes aboard a vessel centuries from port.',
  'terraform ecologist':
    'Manages engineered biospheres and their unintended emergents.',
  'physicist who knows too much':
    'Understands the mechanism behind the crisis — and the deadline it implies.',
  'salvage crew chief':
    'Bosses hazardous recovery ops where the wreck pays or kills.',
  'sidekick ready to lead':
    "The assistant whose competence finally outruns their mentor's mandate.",
  'hero without public trust':
    'Saves cities nobody thanks; PR battles harder than villains.',
  'legacy name carrying someone new':
    "Inherits a famous mantle they didn't earn yet.",
  'burned-out veteran mentor':
    'Washed fighter teaching the next wave, dodging their own ghosts.',
  'profiler with boundary issues':
    'Reads killers too well; empathy bleeds into obsession.',
  'questioner of inherited beliefs':
    'Raised in a doctrine, now auditing it from inside.',
  'caregiver losing and finding themselves':
    'Long-term nurse of a relative, recovering an identity beyond duty.',
  'overprepared planner meets chaos':
    'Has contingencies for everything except what actually happens.',
  'best friend waiting too long':
    'The orbiting friend whose patience converts to confession late.',
  'fake partner with real cracks':
    'Contractual romance failing at pretending to be fake.',
  'matriarch or patriarch holding a fractured family':
    'Elder keeping squabbling kin under one roof and one will.',
  'coming-of-age everyteen on a threshold':
    'Ordinary adolescent at the exact hinge between school and life.',
  'ordinary person facing an unremarkable crisis':
    'Illness, layoff, divorce — quiet catastrophe without spectacle.',
  'idealist ground down by an institution':
    'New hire discovering the machine predates and outlasts them.',
  'griever rebuilding a life after loss':
    'Post-bereavement reconstruction: routines, triggers, small returns.',
  'ex-special forces operator':
    'Elite skills in civilian life looking for a legitimate outlet.',
  'bounty hunter with a code':
    'Collects bounties, but certain contracts get declined.',
  'undercover cop going dark':
    'Deep-cover officer whose handlers may have forgotten them.',
  'martial artist chasing one title':
    'A single belt, bout or championship organizes an entire career.',
  'bodyguard out of hours':
    'Protection professional off-duty — the charge follows them home.',
  'reluctant getaway driver':
    'Only drives because family depends on the fee.',
  'lie that needs three more lies':
    'Comedic escalation of maintenance costs on one fabrication.',
  'butler running the whole scheme':
    'The staff member secretly orchestrating household events.',
  'it person who has seen everything':
    'Sysadmin with total visibility and zero authority.',
  'grieving family sorting an estate':
    'Death-administration drama: boxes, siblings and hidden rooms.',
  'everyman facing an impossible permit':
    'Bureaucratic odyssey of one citizen vs one form.',
  'summoned exchange student':
    "Isekai'd scholar treated as cultural transfer with catastrophic grading.",
  'trapped game developer':
    'Author imprisoned in their own unfinished build.',
  'party face with logistics brain':
    'The socialite who is actually coordinating everything.',
  'second-chance parent':
    'Regressed/reborn adult getting custody of their own childhood back.',
  'veteran in a young body':
    'Old soldier returned to adolescent hardware and second-round wars.',
  'insider trader of fate':
    'Exploits foreknowledge of destiny like market information.',
  'ex-hero starting over quietly':
    'Retired savior rebuilding anonymous domestic life.',
  'otomate script knower':
    'Knows every route of the game world they inhabit.',
  'servant network handler':
    'Runs the staff intel web beneath aristocratic floors.',
  'duelist learning ballroom law':
    'Sword prodigy forced to master etiquette as equally lethal rules.',
  'heroine rewriting her route':
    'The lead girl edits her own prescribed romance storyline.',
  "captured target's confused sister":
    'Dragged into a spy/heist plot as the wrong hostage-relative.',
  'etiquette coach turned player':
    'Manners instructor who starts playing the great-game themselves.',
  'broken-engagement socialite':
    'Jilted aristocrat rebuilding position from scandal.',
  'last-seat strategist':
    'Ranked dead-last, wins through pure planning.',
  'supply-runner optimizer':
    'Logistics maverick proving quartermasters win wars.',
  'reluctant solo carry':
    'Support player forced to become the carry.',
  'audience-favorite puppet':
    "Idol whose votes are manufactured but feelings aren't.",
  'arena ethics defector':
    'Gladiator/champion quitting the system that made them.',
  'floor-rookie learning rules':
    'First-year tower climber absorbing floor-by-floor law.',
  'gatekeeper whisperer':
    'Charisma specialist who charms guards, clerks and thresholds.',
  'checkpoint abuser with guilt':
    'Exploits respawn/fast-travel mechanics while mourning the shortcuts.',
  'vertical guild runner':
    'Courier specializing in tower verticality — the elevator economy.',
  'summit myth believer':
    "Ascends purely to prove the peak's legend true.",
  'nascent dungeon core':
    'Newly awakened intelligence learning to grow a dungeon responsibly.',
  'hero-turned-core diplomat':
    'Former adventurer negotiating peace between cores and clears.',
  'monster roster manager':
    'HR director for dungeon creatures: spawns, wages, morale.',
  'loot economy fixer':
    'Repairs broken reward markets that pit adventurers against each other.',
  'adventurer guild liaison':
    'Bridge between guild offices and reckless freelancers.',
  'mechanic keeping the frame alive':
    'Maintains the giant robot/mech that everyone else pilots.',
  'colony kid in a warframe':
    'Child conscripted into a protective combat chassis.',
  'untaught country transfer':
    'Rural newcomer mocked for gaps, revealed for strengths.',
  'third-year last season':
    "Final high-school year pressing every teammate's stakes.",
  'former pro-turned-coach failure':
    "Washed star coaching the team they can't beat anymore.",
  'rival generation handoff kid':
    'Teen groomed (or resisting) to replace a retiring legend.',
  'cafe regular becoming staff':
    'Customer who quietly transitions into belonging there.',
  'new tenant in an old building':
    "Mover inheriting the superstructure's ghosts and neighbors.",
  'freelancer relearning routine':
    'Gig worker rebuilding structure after burnout.',
  'quiet mentor with no agenda':
    'Guide who asks nothing back — rarest role in the story.',
  'transfer student reading the room':
    "New pupil decoding a classroom's existing power map.",
  'club president holding factions':
    'Student leader balancing warring subgroups of one club.',
  'delinquent with hidden grades':
    'Troublemaker whose transcripts secretly top the year.',
  'jealous number one':
    'Top-ranked student threatened by the natural #2.',
  'the one who reads flags':
    'Friend who notices romantic signals everyone misses.',
  'newcomer archetype disruptor':
    'Transfer whose mere existence breaks stock character assignments.',
  'negotiating housemate':
    'Roommate resolving chore/rent treaties like mini summits.',
  'pervert with a hidden code':
    'Comic degenerate who draws one firm, sincere line.',
  'straight-man surrounded by chaos':
    'The only sane voice in an ensemble of eccentrics.',
  'unwitting harem magnet':
    'Normal teen accumulating admirers through sheer obliviousness.',
  'tease escalation rival':
    "Mutual provocation spiraling past either's intent.",
  'ally gathered one debt at a time':
    'Party assembled not by charisma but by owed favors.',
  'prosecutor against the institution':
    'Legal champion choosing to indict their own system.',
  'target mirror of the avenger':
    'The revenge list includes someone who was the original victim.',
  'ordinary middle-schooler recruited':
    'Puberty-age kid drafted into magic/sport/spy work.',
  'retired magical girl mentoring':
    "Past heroine training the replacement she hopes doesn't repeat her.",
  'rival witch with sympathetic goal':
    'Antagonist whose aims are justifiable — inconveniently.',
  'contract familiar with hidden agenda':
    'Bound companion serving two contracts at once.',
  'self-taught street mage':
    'Wizard educated by necessity, not academy — wild, effective, illegal.',
  'academy dropout practicing alone':
    'Ex-student whose expelled techniques still work fine.',
  'support-mage strategist':
    "Healer/buffer who actually commands the party's tactics.",
  'inheritor of a dying school':
    'Last student of a fading tradition, charged with reviving or burying it.',
  'chosen one':
    'Prophesied savior carrying an expectation they did not choose — destiny as burden.',
  'royal heir':
    'Next in line to a throne: legitimacy, grooming and siblings as rivals.',
  'prophesied one':
    'Named in scripture before birth; every action read as fulfillment or betrayal of text.',
  'military officer':
    'Command responsibility within armed forces — orders upward, lives downward.',
  'merchant captain':
    'Trades who commands ship, cargo and crew; profit margins as strategy.',
  'explorer':
    'Professionally curious: mapping, contact and return with knowledge others lack.',
  'rebel leader':
    'Insurgent commander holding a fragile coalition together against a common state.',
  'imperial noble':
    'Court insider whose power derives from proximity to the throne and family name.',
  'amateur detective':
    'Civilian pulled into investigation by curiosity, grief or stubbornness — method learned on the job.',
  'librarian':
    'Keeper of organized knowledge; catalog skills become investigative superpowers.',
  'retired professional':
    'Former expert dragged back by a problem only their old craft can solve.',
  'intelligence officer':
    'Career analyst/operator inside agencies: assessment, clearance and quiet influence.',
  'career professional':
    'Defined by steady competence in an ordinary field until circumstance asks more.',
  'teacher/academic':
    'Educator or researcher whose expertise collides with institutional or real-world stakes.',
  'investigator':
    'Licensed or obsessive seeker of facts — insurance, corporate or private.',
  'innocent victim':
    'Wrongly caught in events; the story follows dignity under undeserved suffering.',
  'cursed person':
    'Living with an active affliction whose rules dictate daily existence.',
  'tribal leader':
    'Chief or elder steering a kin-based community through change and external pressure.',
  'ancient scholar':
    'Learned mind from a lost era preserving texts the present cannot read.',
  'frontier doctor':
    'Physician practicing beyond adequate infrastructure — improvisation as standard of care.',
  'regressor':
    'Returned deliberately to an earlier point in their own timeline with adult knowledge.',
  'hardworking solo player':
    'Grinds progression alone through discipline rather than talent or luck.',
  'barista/baker owner':
    'Small shop keeper whose counter anchors the neighborhood\'s daily life.',
  'paramedic':
    'Emergency responder living in the golden hour between accident and outcome.',
  'junior officer':
    'Lowest command rung: respected barely, responsible fully.',
  'medic':
    'Combat or expedition healer — triage under fire.',
  'mechanic/pilot':
    'Builds and flies; technical intimacy with the machine doubles as identity.',
  'settlement medic':
    'Only trained healer in a remote colony — every case is a policy decision.',
  'ex-soldier':
    'Veteran reintegrating while tactical reflexes keep reading civilian life as terrain.',
  'wanderer':
    'Purposeful drifter: movement as answer to questions they won\'t say aloud.',
  'engineer hoarding tech':
    'Practical builder stockpiling forbidden or obsolete components against the collapse.',
  'archaeologist':
    'Excavates the material past professionally; digs frequently disturb the living present.',
  'cartographer':
    'Mapmaker whose charts become claims, treasures and targets.',
  'shipwreck survivor':
    'Castaway relearning civilization from the beach outward.',
  'sect heir':
    'Child groomed to lead a cultivation school torn between filiation and reform.',
  'beggar-gang prodigy':
    'Street orphan whose raw talent recruits them upward through criminal networks.',
  'assassin seeking redemption':
    'Killer attempting to spend remaining life offsetting a ledger of kills.',
  'outer-sect underdog':
    'Lowest-ranked disciple in a hierarchical academy grinding toward recognition.',
  'reincarnated ancestor':
    'Reborn elder of their own bloodline, correcting mistakes only they remember.',
  'clan young master':
    'Aristocratic scion whose privilege is simultaneously armor and cage.',
  'retired teacher':
    'Former educator drawn back by one student, one injustice or one unfinished lesson.',
  'village baker':
    'Humble tradesperson whose oven anchors community rhythms and confessions.',
  'cat-loving librarian':
    'Quiet custodian whose orderliness shelters unexpected courage.',
  'retired mastermind':
    'Former strategist/criminal architect pulled out of seclusion by an equal-class threat.',
  'grifter charming':
    'Likeable con artist whose real skill is making marks feel chosen.',
  'community engineer':
    'Builder solving collective problems — pumps, bridges, grids — against budgets and politics.',
  'rewilder':
    'Restoration practitioner reintroducing species and processes to damaged land.',
  'seed librarian':
    'Custodian of a seed vault guarding agriculture\'s genetic options.',
  'hex-broken heiress':
    'Inheritor stripped of fortune by a curse or scandal, rebuilding without the name.',
  'fae prince in exile':
    'Immortal royalty navigating mortal courts after losing the otherworld.',
  'scholar of forbidden magic':
    'Academic studying exactly the disciplines the establishment banned.',
  'compliance officer doubting orders':
    'Corporate regulator whose diligence turns against the company itself.',
  'starship captain':
    'Master of vessel and crew in deep space — command measured in light-years.',
  'freighter pilot':
    'Blue-collar void trucker hauling cargo along dangerous lanes.',
  'heir to a merchant dynasty':
    'Successor to trading wealth where family business and family feud are identical.',
  'cartographer turned treasure hunter':
    'Mapmaker using professional knowledge to chase what they once only charted.',
  'noble\'s ward ran away to sea':
    'Protected ward trading guardianship for berth, rank and risk.',
  'reformed pirate quartermaster':
    'Ex-pirate administrator applying illicit logistics to lawful enterprise.',
  'first mate against their convictions':
    'Number two executing orders they privately believe are wrong.',
  'likeable con artist one job from free':
    'Grifter whose exit plan keeps requiring one more score.',
  'servant outsmarting every master':
    'Household staff whose invisibility is the sharpest intellect in the house.',
  'orphan running scams to survive':
    'Parentless kid using cons as a welfare system.',
  'retired rogue training an heir':
    'Former scoundrel teaching the next generation tricks they survived by.',
  'inheritor of the cursed estate':
    'New owner of property that punishes possession itself.',
  'skeptic doctor facing the impossible':
    'Evidence-trained physician confronted with cases evidence cannot hold.',
  'ambitious scholarship student':
    'Merit-scholarship newcomer determined to convert study into escape velocity.',
  'airship engineer':
    'Maintainer of lighter-than-air craft where every gram is a design argument.',
  'drought farm heir':
    'Young inheritor of failing land during water collapse.',
  'reservoir engineer':
    'Water-systems designer mediating between upstream towns and downstream farms.',
  'heatwave paramedic':
    'First responder working climate emergencies at medical capacity limits.',
  'diaspora returnee architect':
    'Exiled designer coming home to rebuild — literally and politically.',
  'systems engineer on thin margins':
    'Infrastructure keeper holding complex stacks together with too few resources.',
  'late-debut hero':
    'Savior whose call arrived decades after everyone else peaked.',
  'villain trying to retire clean':
    'Retired antagonist negotiating amnesty with a world that remembers everything.',
  'by-the-book detective':
    'Procedure-first investigator whose rigidity is also incorruptibility.',
  'forensics specialist who sees patterns':
    'Lab analyst connecting scenes through data invisible to detectives.',
  'patrol officer promoted too fast':
    'Beat cop elevated beyond readiness, learning command under scrutiny.',
  'unreliable witness of their own life':
    'Narrator whose memory of their own story is demonstrably edited.',
  'returnee facing a changed home':
    'Emigrant coming back to find the place optimized or ruined without them.',
  'quiet professional near a breaking point':
    'Steady worker whose composure has a countdown nobody scheduled.',
  'cynical romantic under cover of jokes':
    'Believer in love disguised as skeptic through comedic deflection.',
  'rival who secretly reads their work':
    'Competitor whose antagonism conceals genuine admiration and attention.',
  'new hire taking memos literally':
    'Junior employee exposing corporate euphemism by obeying it exactly.',
  'reincarnated actuary':
    'Risk analyst reborn applying actuarial thinking to fantasy catastrophe tables.',
  'refusenik chosen one':
    'Prophesied savior who declined — and lives with the world\'s reaction.',
  'local guide teaching the outsider':
    'Resident mentor reversing the discovery narrative: expertise stays home.',
  'regressor counting known disasters':
    'Time-returner tracking apocalypses they already memorized.',
  'reformed storybook villainess':
    'Villain archetype rewriting her own role mid-story.',
  'engagement-deadline heiress':
    'Heiress whose marriage contract expires before she finds a real choice.',
  'captured target\'s confused sister':
    'Kidnap-adjacent relative mistaken for the objective and swept into the plot.',
  'scholar of the scripted world':
    'Researcher analyzing their reality\'s narrative mechanics from inside.',
  'ace pilot burned by command':
    'Elite aviator discarded by the hierarchy that built them.',
  'test pilot promoted too fast':
    'Experimental flyer advanced before the airframe or age justified it.',
  'sync-mediated child soldier':
    'Minor piloting through neural link — childhood filtered through machine compatibility.',
  'walk-on bench grinder':
    'Sports newcomer earning minutes through pure repetition.',
  'returnee to a changed hometown':
    'Veteran or migrant finding home retrofitted in their absence.',
  'exam-pressure prodigy':
    'Gifted student whose talent is managed entirely through test ranking.',
  'teacher fighting administration':
    'Classroom advocate battling district policy directly.',
  'oblivious shared-goal leader':
    'Commander uniting people perfectly while missing every interpersonal signal.',
  'professional who cannot take a joke':
    'Expert whose seriousness becomes the ensemble\'s straight man.',
  'fallen heir rebuilding evidence':
    'Dispossessed successor reconstructing proof of their claim.',
  'disguised returnee':
    'Homecoming under a false name to observe before revealing.',
};
