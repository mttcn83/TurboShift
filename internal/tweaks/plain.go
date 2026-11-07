package tweaks

// Plain-language text for the catalogue.
//
// Each tweak is written once by its author in precise, technical terms (kernel
// attribute names, the exact setting it writes). That text is kept as
// Meta.Technical. What people read first, Meta.Details and the option labels
// and notes, comes from this table, in everyday words: what the setting does
// and what you would notice, not how it is implemented. Anything missing from
// the table simply keeps its original text, so adding a tweak never needs an
// entry here to work.

// pt is a plain-language option text. An empty label keeps the original one.
type pt struct{ label, note string }

type plainText struct {
	details string
	opts    map[string]pt
}

// plained returns m with its everyday-language wording applied.
func plained(m Meta) Meta {
	p, ok := plainTexts[m.ID]
	if !ok {
		return m
	}
	if m.Technical == "" {
		m.Technical = m.Details
	}
	if p.details != "" {
		m.Details = p.details
	}
	m.Options = plainOpts(m.ID, m.Options)
	return m
}

// plainOpts rewrites option labels and notes for tweak id, leaving values alone.
func plainOpts(id string, in []Option) []Option {
	p, ok := plainTexts[id]
	if !ok || len(in) == 0 {
		return in
	}
	out := make([]Option, len(in))
	for i, o := range in {
		if t, ok := p.opts[o.Value]; ok {
			if t.label != "" {
				o.Label = t.label
			}
			if t.note != "" {
				o.Note = t.note
			}
		}
		out[i] = o
	}
	return out
}

// pollNotes are shared by the three USB report-rate tweaks.
var pollNotes = map[string]pt{
	"0": {"Device default", "Let each device pick its own rate."},
	"1": {"1000 times a second", "The fastest rate USB allows, so the least delay between your hand and the screen."},
	"2": {"500 times a second", "Half as often. Use it if a device acts up at the fastest rate."},
	"4": {"250 times a second", "A quarter as often."},
	"8": {"125 times a second", "The standard rate for many ordinary devices."},
}

var plainTexts = map[string]plainText{
	"power.profile": {
		details: "Switches your computer's overall power mode, the same one you see in your desktop's power menu. A faster mode runs hotter, louder and drains the battery quicker; a saving mode does the opposite.",
		opts: map[string]pt{
			"performance": {"Performance", "Fastest response. More heat, fan noise and battery drain."},
			"balanced":    {"Balanced", "The usual setting for everyday use."},
			"power-saver": {"Power saver", "Uses the least power. Things can feel noticeably slower."},
		},
	},
	"cpu.governor": {
		details: "Chooses how quickly the processor speeds up and slows down. 'Always fast' keeps it running at high speed all the time; the saving choices let it rest whenever it can.",
		opts: map[string]pt{
			"performance":  {"Always fast", "Keep the processor at high speed all the time."},
			"powersave":    {"Save power", "Let the processor slow down as much as it can."},
			"schedutil":    {"Follow the workload", "Speed up and slow down along with what the system is doing."},
			"ondemand":     {"Speed up when busy", "Slow when idle, fast as soon as there is work."},
			"conservative": {"Change gradually", "Speed up and slow down in small steps."},
		},
	},
	"cpu.epp": {
		details: "Tells the processor whether it should lean towards speed or towards saving power. If it leans towards saving power while you are plugged in, it can be slow to speed up, which can feel like lag.",
		opts: map[string]pt{
			"performance":         {"Favour speed", "Always lean towards speed. Most heat and power use."},
			"balance_performance": {"Lean towards speed", "Mostly speed, but idle parts of the chip still save power."},
			"default":             {"Your PC's own choice", "Whatever the manufacturer set."},
			"balance_power":       {"Lean towards saving power", "Slower to speed up, uses less power."},
			"power":               {"Save power", "Strongly saves power; the processor is slow to speed up."},
		},
	},
	"cpu.boost": {
		details: "Lets the processor temporarily run faster than its normal speed when there is spare cooling and power. Turning it off keeps the chip cooler and quieter, but games and compiling can get noticeably slower.",
		opts: map[string]pt{
			"on":  {"On", "Allow short bursts of extra speed when there is room to do it."},
			"off": {"Off", "Never go above normal speed: cooler and quieter, but slower."},
		},
	},
	"cpu.min-perf": {
		details: "Stops the processor from resting at its very slowest when nothing is happening, so it does not have to speed up from a standstill when you start something. The price is more power and heat while idle.",
		opts: map[string]pt{
			"50":  {"Half speed or more", "Never rests below half of its speed range: reacts quicker, uses more power when idle."},
			"75":  {"Three-quarter speed or more", "Reacts quickly, but runs noticeably warmer when idle."},
			"100": {"Full speed", "Runs flat out all the time."},
		},
	},
	"cpu.hwp-boost": {
		details: "When a program that was waiting (for a disk, say) gets its data, the processor jumps to a higher speed straight away instead of working up to it.",
		opts: map[string]pt{
			"1": {"On", "Jump to a high speed as soon as a waiting program wakes up."},
			"0": {"Off", "The normal behaviour."},
		},
	},
	"platform.profile": {
		details: "Many laptops have a built-in mode that controls fan noise and how much power the chip may draw. This switches it. Louder and hotter modes allow more performance for longer.",
		opts: map[string]pt{
			"low-power":            {"Low power", "Quietest and coolest, with the lowest performance."},
			"quiet":                {"Quiet", "Less fan noise."},
			"cool":                 {"Cool", "Keeps temperatures lower."},
			"balanced":             {"Balanced", "The laptop's normal setting."},
			"balanced-performance": {"Balanced, leaning fast", "More performance with a little more heat and noise."},
			"performance":          {"Performance", "The most performance the laptop allows. Loudest."},
		},
	},
	"cpu.autogroup": {
		details: "Keeps one heavy job, like compiling a big program, from hogging the processor so much that the rest of your desktop gets sluggish. It is usually already on; this lets you check it or put it back.",
		opts: map[string]pt{
			"1": {"On", "Heavy jobs share the processor fairly with what you are doing. Usually the default."},
			"0": {"Off", "Every program competes on its own."},
		},
	},
	"sys.watchdog": {
		details: "Turns off a safety check that wakes every processor core every few seconds to see if it has frozen. Switching it off saves a tiny amount of power and removes rare, very small hiccups, but if the whole machine freezes you will get no message about why.",
		opts: map[string]pt{
			"0": {"Off", "No freeze check: fewer wake-ups and a little less power use."},
			"1": {"On", "Keep the freeze check running. Usual on many systems."},
		},
	},
	"pcie.aspm": {
		details: "Parts inside your computer (the storage drive, graphics card, network card) are connected by links that doze off to save power and take a moment to wake up. This keeps them awake so they answer sooner, at the cost of more power and heat when idle.",
		opts: map[string]pt{
			"default":        {"Your PC's own setting", "Use the power saving your computer's manufacturer chose."},
			"performance":    {"Keep awake", "Never let the links doze: quickest answers, more idle power."},
			"powersave":      {"Save power", "Let the links doze often."},
			"powersupersave": {"Save the most power", "Doze as much as possible. Can add small delays."},
		},
	},
	"usb.autosuspend": {
		details: "USB devices such as mice, keyboards and headsets switch themselves off when unused to save power, and take a moment to wake. That can feel like a missed first click or a crackle in a headset. This stops them from switching off.",
		opts: map[string]pt{
			"-1": {"Never switch off", "Devices stay powered, so there is no wake-up delay."},
			"2":  {"After 2 seconds", "The usual setting: idle devices switch off after 2 seconds."},
			"10": {"After 10 seconds", "Devices switch off only after 10 idle seconds."},
		},
	},
	"input.mouse-poll": {
		details: "Controls how many times a second a USB mouse reports its position. More often means less delay between moving your hand and seeing it on screen. Gaming mice often do this already; this sets it for devices that default to a slower rate. It affects devices you plug in afterwards.",
		opts:    pollNotes,
	},
	"input.kb-poll": {
		details: "Controls how many times a second a USB keyboard reports your key presses. More often means less delay before the computer sees them. It affects devices you plug in afterwards.",
		opts:    pollNotes,
	},
	"input.pad-poll": {
		details: "Controls how many times a second a USB game controller reports its buttons and sticks. More often means less delay before the game sees your input. It affects devices you plug in afterwards.",
		opts:    pollNotes,
	},
	"net.wifi-powersave": {
		details: "Wi-Fi adapters normally doze between messages to save battery, which can add small, uneven delays. This keeps the Wi-Fi awake. It helps with a good signal and does nothing for a weak one. It uses more battery.",
		opts: map[string]pt{
			"off":     {"Off", "Wi-Fi stays awake. Can smooth out small delays on a good signal, but uses more battery."},
			"on":      {"On", "Wi-Fi dozes between messages to save battery."},
			"default": {"Your system's choice", "Let the network manager decide."},
		},
	},
	"net.dns": {
		details: "Switches which service looks up website and game-server addresses for you. This changes how quickly names are found, not how fast your connection is. The service you pick sees every name you look up.",
		opts: map[string]pt{
			"automatic":  {"Automatic (your router)", "Use the lookup service your network provides. The normal setting."},
			"cloudflare": {"Cloudflare 1.1.1.1", "A public lookup service run by Cloudflare. They see the names you look up."},
			"quad9":      {"Quad9 9.9.9.9", "A public lookup service that blocks known harmful sites. They see the names you look up."},
			"google":     {"Google 8.8.8.8", "A public lookup service run by Google. They see the names you look up."},
		},
	},
	"net.tcp-cc": {
		details: "Chooses how your computer decides how fast to send data on a connection that loses data or is far away. Some methods cope better with Wi-Fi, mobile and long-distance links. It affects downloads and web pages, not online games (which use a different kind of traffic).",
		opts: map[string]pt{
			"bbr":      {"BBR", "A newer method that often does better on lossy or distant links. It can be pushy towards other devices sharing your connection."},
			"cubic":    {"CUBIC", "The usual choice on most Linux systems."},
			"reno":     {"Reno", "An old, cautious method."},
			"westwood": {"Westwood", "Made for wireless links that lose data."},
			"htcp":     {"H-TCP", "Made for very fast, long-distance links."},
			"vegas":    {"Vegas", "Backs off early when delay grows, so it gives way to other traffic."},
		},
	},
	"net.qdisc": {
		details: "When a download or upload fills your connection, everything else waits in a queue, and your ping shoots up. This changes how the queue is managed so that games and calls stay responsive while something big is transferring. It applies to connections made afterwards, so reconnect to use it now.",
		opts: map[string]pt{
			"fq_codel":   {"fq_codel", "Shares the connection fairly and keeps delay low. The usual choice."},
			"cake":       {"cake", "Adds extra fairness between devices to the usual choice. Needs a part that may not be loaded."},
			"fq":         {"fq", "Fair sharing without delay control. Mostly for servers."},
			"pfifo_fast": {"pfifo_fast", "A plain queue with no delay control. Old-fashioned."},
		},
	},
	"net.tcp-fastopen": {
		details: "Lets your computer send its first piece of data together with the request to connect to a website it has visited before, saving a short wait. It only helps with sites that support it, and saves a fraction of a second per connection.",
		opts: map[string]pt{
			"0": {"Off", "Never use this shortcut."},
			"1": {"Outgoing connections", "Use the shortcut when you connect to a site that supports it."},
			"3": {"Outgoing and incoming", "Use it when connecting out, and also accept it from others."},
		},
	},
	"net.tcp-idle": {
		details: "After a connection sits quiet for a moment, your computer normally slows it down and works back up to speed. This stops that, which helps things that send in short bursts, like remote desktop, chat and streaming.",
		opts: map[string]pt{
			"0": {"Stay fast", "A connection that pauses keeps its speed."},
			"1": {"Slow down again", "The usual behaviour: speed is rebuilt after a pause."},
		},
	},
	"net.notsent-lowat": {
		details: "Limits how much data a program can pile up waiting to be sent on a connection, so that fresh data does not wait behind a long backlog. This can cut lag on remote desktop, SSH and game streaming, but a very small limit can slow large uploads.",
		opts: map[string]pt{
			"131072":     {"A moderate limit", "Less lag on interactive connections with little effect on big transfers."},
			"16384":      {"A tight limit", "Lowest delay for chatty connections. Can slow big uploads."},
			"4294967295": {"No limit", "The usual behaviour."},
		},
	},
	"net.buffers": {
		details: "Raises how much data a single connection may have on its way at once. On a very fast connection or a distant server, a small limit stops one download from reaching full speed. The cost is more memory when many large transfers run together.",
		opts: map[string]pt{
			"default": {"Usual size", "Fine for most home connections."},
			"fast":    {"Large", "Lets fast or far-away connections reach full speed. Uses more memory."},
		},
	},
	"net.budget": {
		details: "On a very fast network connection, your computer can fall behind handling packets as they arrive, which adds small delays. This lets it handle more in one go. It only matters on very busy or very fast (2.5 Gb/s and up) connections.",
		opts: map[string]pt{
			"300":  {"Usual amount", "The normal setting."},
			"600":  {"More at once", "Handle more packets each time. Less overhead on fast connections."},
			"1000": {"A lot more at once", "Only for extremely busy connections."},
		},
	},
	"net.backlog": {
		details: "When data arrives faster than your computer can handle it, it waits in a line, and anything that does not fit is thrown away and has to be sent again. This makes the line longer so short bursts are not lost. It only matters on very fast or busy connections.",
		opts: map[string]pt{
			"1000":  {"Usual length", "The normal setting."},
			"5000":  {"Longer", "Absorb bigger bursts before dropping anything."},
			"10000": {"Much longer", "For 10 Gb/s connections and heavy traffic."},
		},
	},
	"net.mtu-probing": {
		details: "Some networks (VPNs, mobile hotspots, hotel Wi-Fi) quietly lose large pieces of data, so a page half-loads or a remote session freezes. With this on, your computer notices and switches to smaller pieces by itself. On a healthy network it normally does nothing.",
		opts: map[string]pt{
			"0": {"Off", "The usual behaviour."},
			"1": {"Only when stuck", "Try smaller pieces only after a connection looks stuck."},
			"2": {"Always", "Always test for the best size. Sends a few extra test packets."},
		},
	},
	"net.ecn": {
		details: "Lets a busy router say \"slow down\" by marking data instead of throwing it away, so nothing has to be re-sent. A few old routers and firewalls handle this badly, which can make some sites slow to connect.",
		opts: map[string]pt{
			"0": {"Off", "Never use this."},
			"1": {"Always ask for it", "Ask for it on every outgoing connection."},
			"2": {"Only if the other side asks", "The usual behaviour."},
		},
	},
	"mem.swappiness": {
		details: "Decides how eagerly your computer moves memory you are not using onto the disk (swap) to free up room. A low number keeps your programs in memory longer; a high number is for fast swap such as compressed memory. There is no best number for everyone.",
		opts: map[string]pt{
			"10":  {"Keep in memory", "Move things to disk only when memory is nearly full."},
			"30":  {"Lean towards keeping in memory", "Prefer to keep programs in memory."},
			"60":  {"Usual", "The normal setting."},
			"100": {"Even", "Treat disk swap and cached files equally. Sensible with compressed memory."},
			"180": {"Move out eagerly", "Move idle things out early. Meant for fast swap such as compressed memory."},
		},
	},
	"mem.zswap": {
		details: "Instead of writing memory you are not using straight to a slow disk, your computer squeezes it into a small area of memory first and only writes what does not fit. Getting it back is much faster than reading it from disk. It does nothing if you have no swap.",
		opts: map[string]pt{
			"1": {"On", "Squeeze memory first. Far fewer slow disk writes when memory runs low."},
			"0": {"Off", "Write straight to the swap disk."},
		},
	},
	"mem.zswap-compressor": {
		details: "Chooses how that memory squeezing is done. A faster method gets things back with less delay; a stronger method fits more into the same space but uses more processor time.",
		opts: map[string]pt{
			"lz4":     {"lz4 (fast)", "Very fast with moderate squeezing. Best for responsiveness."},
			"zstd":    {"zstd (strong)", "Squeezes more but uses more processor time."},
			"lzo":     {"lzo (light)", "Fast and light, a bit weaker than lz4."},
			"lzo-rle": {"lzo-rle (light)", "Like lzo, good for memory that is mostly empty."},
		},
	},
	"mem.page-cluster": {
		details: "When your computer brings memory back from swap, it reads several pieces at once, which suits spinning hard disks. On fast swap such as an SSD or compressed memory, reading just what is needed wastes less and can shorten freezes.",
		opts: map[string]pt{
			"0": {"Just what is needed", "Lowest delay. Best for fast swap."},
			"1": {"2 pieces", "Read a couple of pieces together."},
			"3": {"8 pieces", "The usual setting. Helps spinning disks."},
		},
	},
	"mem.vfs-cache": {
		details: "Your computer remembers where files and folders are so it can find them again quickly. This keeps that memory longer, which speeds up browsing big folders, game libraries and project trees. It uses memory that programs could otherwise have.",
		opts: map[string]pt{
			"50":  {"Remember longer", "Faster repeated file lookups if you have spare memory."},
			"100": {"Usual", "The normal setting."},
			"200": {"Forget sooner", "Frees memory for programs sooner."},
		},
	},
	"mem.compaction": {
		details: "In the background, your computer tidies memory to keep big free blocks available, and on some PCs that tidying shows up as an occasional hiccup in games. Turning it off removes that background work; big memory requests may then take a little longer.",
		opts: map[string]pt{
			"0":  {"Off", "No background tidying."},
			"20": {"Usual", "The normal setting."},
			"50": {"More eager", "Tidy more, which helps programs that need big blocks."},
		},
	},
	"mem.thp": {
		details: "Memory can be handed out in small pieces or big ones. Big pieces make memory-hungry programs (databases, emulators, some games) a bit faster, but the system sometimes pauses to make room for them and programs can use more memory than they need. Neither choice is better for everyone.",
		opts: map[string]pt{
			"always":  {"Always use big pieces", "Can speed up some programs, but can cause pauses and extra memory use."},
			"madvise": {"Only when a program asks", "The usual setting on Arch and Ubuntu."},
			"never":   {"Never", "Steadier timing, a little slower for memory-hungry programs."},
		},
	},
	"mem.thp-defrag": {
		details: "When a program asks for memory in big pieces, your computer may pause it while it tidies memory to make room. This moves that tidying into the background so programs do not have to wait. A few memory-hungry programs may run slightly slower.",
		opts: map[string]pt{
			"madvise":       {"Wait only when asked", "Pause only programs that asked for big pieces. The usual setting."},
			"defer+madvise": {"Tidy in the background", "Never make programs wait, except those that asked for big pieces."},
			"defer":         {"Always in the background", "Never make programs wait; tidy in the background."},
			"never":         {"Never tidy", "Do not tidy memory for big pieces at all."},
			"always":        {"Make programs wait", "Pause any program until a big piece is ready. Worst for smoothness."},
		},
	},
	"mem.min-free": {
		details: "Keeps a chunk of memory empty at all times so that when a program suddenly needs some (loading a game level, say), the system does not have to stop and clear space first. That memory is no longer available to programs, and too much of it makes things worse.",
		opts: map[string]pt{
			"65536":  {"64 MiB", "A modest amount. Avoids most pauses."},
			"131072": {"128 MiB", "A larger amount for games and big projects."},
			"262144": {"256 MiB", "A large amount. Wasteful on small machines."},
		},
	},
	"mem.watermark-scale": {
		details: "Your computer starts clearing memory in the background once free memory runs low. This makes it start a little earlier, so programs run into a real shortage less often. It means slightly more background work and a little less file memory kept.",
		opts: map[string]pt{
			"10":  {"Usual", "The normal setting."},
			"50":  {"A bit earlier", "Start clearing earlier so programs rarely hit a shortage."},
			"125": {"Much earlier", "Start very early. Meant for machines with lots of memory and bursty programs."},
		},
	},
	"mem.ksm": {
		details: "Your computer can scan memory for identical pieces and keep just one copy, which saves memory but costs processor time. This turns the scanning off. It mainly matters if you run many similar virtual machines; if you do, you lose those savings.",
		opts: map[string]pt{
			"0": {"Off", "Stop scanning: frees a little processor time."},
			"1": {"On", "Keep scanning for identical memory. Useful with many similar virtual machines."},
		},
	},
	"mem.stat-interval": {
		details: "Every processor core briefly wakes up once a second to update memory counters. Doing it less often removes most of those tiny interruptions. The counters in system monitors just update a little slower.",
		opts: map[string]pt{
			"1":  {"Every second", "The usual setting."},
			"5":  {"Every 5 seconds", "Update memory counters less often."},
			"10": {"Every 10 seconds", "Fewest interruptions. Counters lag a little."},
		},
	},
	"io.scheduler": {
		details: "Chooses how your computer orders the requests it sends to your drive. The usual choice suits most machines. Some choices keep the desktop responsive during heavy disk use, and others are best for very fast drives. Change it only if you see the disk making things freeze.",
		opts: map[string]pt{
			"none":        {"No reordering", "Best for fast NVMe drives, which do their own ordering."},
			"mq-deadline": {"mq-deadline (simple)", "Simple and light. A good all-round choice."},
			"bfq":         {"bfq (fair to everything)", "Keeps the desktop responsive under heavy disk use. Uses more processor time."},
			"kyber":       {"kyber (quick answers)", "Aims for quick answers on fast drives."},
		},
	},
	"io.readahead": {
		details: "When a program reads a big file from start to finish, your computer can read ahead so the next part is already there. A bigger amount helps hard disks and network drives with large files. On SSDs and for scattered reads it mostly wastes memory.",
		opts: map[string]pt{
			"128":  {"128 KiB", "The usual amount."},
			"512":  {"512 KiB", "Read further ahead."},
			"1024": {"1 MiB", "Good for big files on hard disks and network drives."},
			"2048": {"2 MiB", "A lot. Wastes memory for scattered reads."},
		},
	},
	"io.rq-affinity": {
		details: "When a drive finishes a job, the answer can be handled by any processor core. This makes sure the same core that asked handles it, which keeps data nearer to where it is used. It can help very fast NVMe drives and can slightly unbalance the cores.",
		opts: map[string]pt{
			"0": {"Any core", "Whichever core happens to get the answer."},
			"1": {"Nearby core", "The usual setting."},
			"2": {"The same core", "Always the core that asked. Can lower delay on fast NVMe drives."},
		},
	},
	"io.nomerges": {
		details: "Your computer normally combines neighbouring drive requests into bigger ones, which helps hard disks a lot. On a very fast NVMe drive, the effort of looking for requests to combine can cost more than it saves. On other drives, switching it off lowers speed on large files.",
		opts: map[string]pt{
			"0": {"Combine all", "The usual setting."},
			"1": {"Only easy ones", "Skip the expensive search for requests to combine."},
			"2": {"Never combine", "Least processor work. Best for fast NVMe drives only."},
		},
	},
	"io.add-random": {
		details: "Your computer uses the exact timing of drive activity as one of many sources of randomness for security. Modern systems have plenty of others, so on an SSD this is a small amount of wasted work. The gain is tiny; you will not notice it on a stopwatch.",
		opts: map[string]pt{
			"0": {"Off", "Do not use drive timing for randomness."},
			"1": {"On", "Use drive timing for randomness. The usual setting for spinning disks."},
		},
	},
	"mem.dirty": {
		details: "When you save or copy files, your computer first keeps the data in memory and writes it to the drive a little later. With lots of memory and a slow drive, a huge pile can build up and then freeze everything while it is written. A smaller pile spreads that out. A bigger pile is faster but risks losing more if the power goes out.",
		opts: map[string]pt{
			"responsive": {"Write sooner", "Start writing early (5% / 10% of memory). Fewer long freezes, especially with USB drives."},
			"default":    {"Usual", "The normal setting (10% / 20% of memory)."},
			"throughput": {"Write later", "Keep much more waiting (20% / 40% of memory). Fewer, bigger writes, but anything not yet written is lost in a power cut."},
		},
	},
	"mem.writeback-timing": {
		details: "Your computer writes saved data to the drive in the background every few seconds. Writing more often keeps the drive busy but means less is lost if the power fails; writing less often lets the drive rest, which saves a little laptop battery.",
		opts: map[string]pt{
			"smooth":  {"Often", "Write every second. Less to lose if something crashes."},
			"default": {"Usual", "Write every 5 seconds. The normal setting."},
			"relaxed": {"Rarely", "Write every 15 seconds. Saves a little laptop battery, but up to a minute of saves can be lost in a power cut."},
		},
	},
	"game.max-map-count": {
		details: "Big games and Windows games running through Wine/Proton ask for a huge number of separate pieces of memory, and the old limit is too low for some of them, so they crash or stutter. Most current systems already raise it. A higher limit also lets a misbehaving program use more.",
		opts: map[string]pt{
			"65530":      {"Old limit (65530)", "The old standard."},
			"262144":     {"262144", "Enough for most games."},
			"1048576":    {"1048576", "What many current systems use."},
			"2147483642": {"Largest possible", "Effectively no limit."},
		},
	},
	"game.split-lock": {
		details: "A few games and their anti-cheat use an unusual instruction that makes your computer deliberately stall that program for a moment, which looks like a freeze. This stops the stall. It also removes a protection: any program you run could then slow down everything else on the machine.",
		opts: map[string]pt{
			"0": {"Off", "Do not slow down programs that do this. Removes a protection."},
			"1": {"On", "The usual setting: such programs are deliberately slowed."},
		},
	},
	"gpu.amd-level": {
		details: "Normally an AMD graphics card slows itself down when it is not busy and speeds up when work arrives, which can show as a stutter at the start of a heavy scene. This keeps it at full speed instead. It does not make the card faster than it can go, and it uses much more power and runs hotter. Set it back when you stop gaming.",
		opts: map[string]pt{
			"auto": {"Automatic", "The card adjusts to what it is doing. The usual setting."},
			"high": {"Always full speed", "No speed-up delay, but much more power and heat."},
			"low":  {"Always slow", "Quiet and cool, but slow."},
		},
	},
	"gpu.intel-min-clock": {
		details: "Intel's built-in graphics rest at a very low speed and speed up when needed. Raising the lowest speed removes the speed-up delay, at the cost of more power and heat while idle. The top speed does not change.",
	},
	"gpu.dgpu-awake": {
		details: "A separate graphics card normally switches off when idle and takes a moment (often a second or more) to wake up. This keeps it switched on so games and apps can use it straight away. It draws power all the time, which cuts laptop battery life.",
		opts: map[string]pt{
			"on":   {"Always on", "Keep the graphics card powered. No wake-up delay."},
			"auto": {"Allow sleep", "The card switches off when idle to save battery. The usual setting."},
		},
	},
	"desktop.hypr-effects": {
		details: "Turns off the fancy effects in the Hyprland desktop (animations, blur and shadows). Blur in particular is costly for the graphics card. The desktop looks flatter and reacts a little more directly. It lasts until you log out.",
		opts: map[string]pt{
			"off": {"Off", "No animations, blur or shadows."},
			"on":  {"On", "Everything as set up in your Hyprland configuration."},
		},
	},
	"desktop.hypr-tearing": {
		details: "Lets a game show each frame the moment it is ready instead of waiting for the screen's next refresh, which removes up to one refresh of delay. The price is that you may see thin horizontal lines (tearing). It only does anything for games you have also set to allow it.",
		opts: map[string]pt{
			"off": {"Off", "Always wait for the screen refresh."},
			"on":  {"On", "Games set to show frames immediately may show tear lines."},
		},
	},
	"desktop.hypr-direct-scanout": {
		details: "When one window fills the whole screen, such as a full-screen game or video, Hyprland can send it straight to the display instead of copying it first. That can save a little time per frame. Rarely, a game or driver shows glitches with it.",
		opts: map[string]pt{
			"off": {"Off", "Always copy through the desktop first."},
			"on":  {"On", "A full-screen window goes straight to the screen."},
		},
	},
	"sys.watchers": {
		details: "Editors, dev servers and sync tools ask your computer to tell them when files change, and your computer limits how many files each user may have watched. When the limit runs out those tools fail with \"too many file watchers\" errors. This raises the limit. It only matters if you hit that error or work in huge folders.",
		opts: map[string]pt{
			"some": {"", "Room for several big projects. Uses system memory only for slots that are actually used."},
			"more": {"", "What editors and sync tools usually ask for."},
			"most": {"", "For very large projects or many at once."},
		},
	},
	"audio.quantum": {
		details: "Sound is delivered in small slices. A bigger slice gives the computer more time per slice, which stops crackles and pops when the computer is busy; a smaller slice cuts the delay between an action and its sound, which matters for music, voice chat and rhythm games. This sets the slice size for all programs at once, until the audio system restarts.",
		opts: map[string]pt{
			"0":    {"Automatic", "Each program picks its own. The usual setting."},
			"128":  {"Small", "Lowest delay. Needs more processor time and crackles sooner if the computer is busy."},
			"512":  {"Medium", "A balance for gaming with voice chat."},
			"1024": {"Large", "Fewest crackles on a busy or slow computer, with more delay."},
		},
	},
	"desktop.hypr-vrr": {
		details: "Normally the monitor refreshes on a fixed beat. Adaptive sync (FreeSync, G-Sync Compatible) lets it refresh whenever the game has a new picture ready, which removes tearing and judder when the frame rate is not steady. It does nothing if your monitor or cable cannot do it.",
		opts: map[string]pt{
			"off":        {"Off", "Fixed refresh rate."},
			"fullscreen": {"Full-screen only", "Adaptive while a full-screen window is up, fixed on the desktop."},
			"on":         {"Always on", "Adaptive all the time. A few monitors flicker on dark scenes."},
		},
	},
	"desktop.gnome-animations": {
		details: "Turns off the movement effects in GNOME and GTK apps (windows sliding, menus fading). Each effect adds a short wait between your click and the result, so without them things feel more abrupt but more direct. It stays off after a restart until you turn it back on.",
		opts: map[string]pt{
			"off": {"Off", "Windows, menus and workspaces change without animation."},
			"on":  {"On", "Animations as designed."},
		},
	},
}
