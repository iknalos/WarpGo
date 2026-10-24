package com.iknalos.warpgo

import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.util.Base64
import com.iknalos.warpgo.warpcore.Warpcore
import org.json.JSONObject

/**
 * Accounts, tests and tunnel control. The tunnels themselves run in the Go core
 * (core/warpcore): WireGuard via wireguard-go, MASQUE via usque.
 */
object WarpManager {

    data class ProbeResult(val ok: Boolean, val ms: Long, val detail: String)

    data class Stats(
        val running: Boolean,
        val mode: Mode?,
        val connected: Boolean,
        val rx: Long,
        val tx: Long,
        val seconds: Int,
    )

    private const val PREFS = "warp_reg"
    private const val K_WG = "wg_account"
    private const val K_MASQUE = "masque_account"
    private const val MEMORY = "auto_memory"

    /** Set by WarpVpnService when a tunnel fails to start; shown once by the UI. */
    @Volatile var lastError: String? = null

    private fun prefs(ctx: Context): SharedPreferences =
        ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    // ---- accounts -------------------------------------------------------------

    /**
     * Blocking. The WireGuard account: the one baked in at build time (works on
     * networks that intercept WARP signup), else a fresh on-device registration.
     */
    fun wireGuardAccount(ctx: Context): String {
        prefs(ctx).getString(K_WG, null)?.let { return it }
        val json = if (BuildConfig.WARP_PRIVATE_KEY.isNotBlank()) {
            JSONObject()
                .put("private_key", BuildConfig.WARP_PRIVATE_KEY)
                .put("address_v6", BuildConfig.WARP_ADDRESS_V6)
                .toString()
        } else {
            Warpcore.registerWireGuard()
        }
        prefs(ctx).edit().putString(K_WG, json).apply()
        return json
    }

    /** Blocking. The MASQUE account: baked in at build time, else registered on the device. */
    fun masqueAccount(ctx: Context): String {
        prefs(ctx).getString(K_MASQUE, null)?.let { return it }
        val json = if (BuildConfig.WARP_MASQUE_CONFIG.isNotBlank()) {
            String(Base64.decode(BuildConfig.WARP_MASQUE_CONFIG, Base64.DEFAULT))
        } else {
            Warpcore.registerMasque()
        }
        prefs(ctx).edit().putString(K_MASQUE, json).apply()
        return json
    }

    fun account(ctx: Context, mode: Mode) =
        if (mode.isMasque) masqueAccount(ctx) else wireGuardAccount(ctx)

    /** The tunnel's own IPv4 and IPv6 addresses, from the account Cloudflare issued. */
    fun tunnelAddresses(account: String, mode: Mode): Pair<String, String?> {
        val o = JSONObject(account)
        val v4 = (if (mode.isMasque) o.optString("ipv4") else o.optString("address_v4"))
            .ifBlank { "172.16.0.2" }.substringBefore('/')
        val v6 = (if (mode.isMasque) o.optString("ipv6") else o.optString("address_v6"))
            .substringBefore('/').ifBlank { null }
        return v4 to v6
    }

    /** Wipe the stored accounts; the next connect uses the baked ones or registers fresh. */
    fun resetRegistration(ctx: Context) {
        prefs(ctx).edit().clear().apply()
    }

    // ---- tests ----------------------------------------------------------------

    /**
     * Blocking. Runs a whole tunnel in memory and checks that a DNS answer comes
     * back through it, so "OK" means data really flows, not just a handshake.
     */
    fun probe(ctx: Context, mode: Mode): ProbeResult = try {
        val account = account(ctx, mode)
        val raw = if (mode.isMasque) {
            Warpcore.probeMasque(account, mode.http2, 6000)
        } else {
            Warpcore.probeWireGuard(account, mode.wgPort.toLong(), 5000)
        }
        val o = JSONObject(raw)
        ProbeResult(o.getBoolean("ok"), o.optLong("ms"), o.optString("detail"))
    } catch (e: Exception) {
        ProbeResult(false, 0, e.message ?: e.toString())
    }

    /** What Auto found working last time on [networkKey], if anything. */
    fun remembered(ctx: Context, networkKey: String?): Mode? =
        networkKey?.let { Mode.fromId(ctx.getSharedPreferences(MEMORY, Context.MODE_PRIVATE).getString(it, null)) }

    fun remember(ctx: Context, networkKey: String?, mode: Mode) {
        if (networkKey == null) return
        ctx.getSharedPreferences(MEMORY, Context.MODE_PRIVATE).edit().putString(networkKey, mode.id).apply()
    }

    /**
     * Identifies the network the phone is on without location permission: its
     * kind plus the DNS servers and search domains it hands out. Null while a
     * VPN is the active network.
     */
    fun networkKey(ctx: Context): String? {
        val cm = ctx.getSystemService(ConnectivityManager::class.java) ?: return null
        val net = cm.activeNetwork ?: return null
        val caps = cm.getNetworkCapabilities(net) ?: return null
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) return null
        val lp = cm.getLinkProperties(net) ?: return null
        val kind = when {
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cell"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "eth"
            else -> "other"
        }
        val dns = lp.dnsServers.mapNotNull { it.hostAddress }.sorted().joinToString(",")
        return "$kind|${lp.domains.orEmpty()}|$dns"
    }

    // ---- tunnel ---------------------------------------------------------------

    /** Starts the tunnel. VPN permission must already be granted (VpnService.prepare). */
    fun connect(ctx: Context, mode: Mode) {
        lastError = null
        ctx.startService(
            Intent(ctx, WarpVpnService::class.java)
                .setAction(WarpVpnService.ACTION_START)
                .putExtra(WarpVpnService.EXTRA_MODE, mode.id)
        )
    }

    fun disconnect(ctx: Context) {
        Warpcore.stop()
        ctx.startService(Intent(ctx, WarpVpnService::class.java).setAction(WarpVpnService.ACTION_STOP))
    }

    fun stats(): Stats {
        val o = JSONObject(Warpcore.stats())
        return Stats(
            running = o.optBoolean("running"),
            mode = Mode.fromId(o.optString("mode")),
            connected = o.optBoolean("connected"),
            rx = o.optLong("rx"),
            tx = o.optLong("tx"),
            seconds = o.optInt("seconds"),
        )
    }

    fun logs(): String = Warpcore.logs()
}
