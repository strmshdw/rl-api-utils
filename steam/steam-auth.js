const SteamUser = require("steam-user");
const readline = require("readline");

// parse CLI flags
function parseArgs() {
    const args = process.argv.slice(2);
    const options = {
        username: process.env.STEAM_USERNAME || process.env.USERNAME || "",
        password: process.env.STEAM_PASSWORD || process.env.PASSWORD || "",
        loginKey: process.env.STEAM_LOGIN_KEY || "",
        accountType: process.env.STEAM_ACCOUNT_TYPE || "Primary Account"
    };

    for (let i = 0; i < args.length; i++) {
        const arg = args[i];
        if (arg === "--username" && i + 1 < args.length) {
            options.username = args[++i];
        } else if (arg === "--password" && i + 1 < args.length) {
            options.password = args[++i];
        } else if (arg === "--login-key" && i + 1 < args.length) {
            options.loginKey = args[++i];
        } else if (arg === "--account-type" && i + 1 < args.length) {
            options.accountType = args[++i];
        }
    }
    return options;
}

const opts = parseArgs();

if (!opts.username) {
    process.stdout.write(JSON.stringify({
        success: false,
        error: "Steam username is required (--username or STEAM_USERNAME)"
    }) + "\n");
    process.exit(1);
}

if (!opts.password && !opts.loginKey) {
    process.stdout.write(JSON.stringify({
        success: false,
        error: "Steam password or loginKey is required"
    }) + "\n");
    process.exit(1);
}

const user = new SteamUser();
let savedLoginKey = opts.loginKey || "";

user.on("loginKey", (key) => {
    savedLoginKey = key;
});

user.on("loggedOn", async () => {
    try {
        // Rocket League Steam App ID = 252950
        const session = await user.createAuthSessionTicket(252950);
        const ticket = Buffer.from(session.sessionTicket).toString("hex").toUpperCase();
        const steamID = user.steamID.getSteamID64();

        const result = {
            success: true,
            sessionTicket: ticket,
            steamID64: steamID,
            loginKey: savedLoginKey,
            accountName: opts.username
        };

        process.stdout.write(JSON.stringify(result) + "\n");
        user.logOff();
        process.exit(0);
    } catch (error) {
        process.stdout.write(JSON.stringify({
            success: false,
            error: "Failed to create auth session ticket: " + error.message
        }) + "\n");
        process.exit(1);
    }
});

user.on("error", (err) => {
    // If loginKey logon failed, fallback to password logon if password is available
    if (opts.loginKey && opts.password && (err.eresult === SteamUser.EResult.InvalidPassword || err.eresult === SteamUser.EResult.AccessDenied)) {
        process.stderr.write(`[${opts.accountType}] Stored Steam login key expired; falling back to password login...\n`);
        opts.loginKey = "";
        user.logOn({
            accountName: opts.username,
            password: opts.password,
            rememberPassword: true
        });
        return;
    }

    process.stdout.write(JSON.stringify({
        success: false,
        error: err.message || "Steam login failed"
    }) + "\n");
    process.exit(1);
});

user.on("steamGuard", (domain, callback) => {
    const prefix = `[${opts.accountType}]`;
    const promptMsg = domain
        ? `${prefix} Steam Guard code needed. Enter code from email (${domain}): `
        : `${prefix} Steam Guard code needed. Enter code from mobile app: `;

    process.stderr.write(promptMsg);

    const rl = readline.createInterface({
        input: process.stdin,
        output: process.stderr
    });

    rl.question("", (code) => {
        rl.close();
        callback(code.trim());
    });
});

// Initiate logon
if (opts.loginKey) {
    user.logOn({
        accountName: opts.username,
        loginKey: opts.loginKey
    });
} else {
    user.logOn({
        accountName: opts.username,
        password: opts.password,
        rememberPassword: true
    });
}
