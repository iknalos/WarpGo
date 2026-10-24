package com.iknalos.warpgo

import android.content.Intent
import android.net.VpnService
import android.os.Build
import com.iknalos.warpgo.warpcore.Warpcore

/**
 * Owns the VPN interface. The tunnel's file descriptor is handed to the Go
 * core, which reads and writes the phone's packets on it.
 */
class WarpVpnService : VpnService() {

    companion object {
        const val ACTION_START = "com.iknalos.warpgo.START"
        const val ACTION_STOP = "com.iknalos.warpgo.STOP"
        const val EXTRA_MODE = "mode"
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> {
                val mode = Mode.fromId(intent.getStringExtra(EXTRA_MODE)) ?: return START_NOT_STICKY
                // Account lookup may hit the network; keep it off the main thread.
                Thread({ start(mode, startId) }, "warpgo-start").start()
            }
            ACTION_STOP -> {
                Warpcore.stop()
                // Only if no newer START is queued: Auto stops and restarts back to back.
                stopSelf(startId)
            }
        }
        return START_NOT_STICKY
    }

    @Synchronized
    private fun start(mode: Mode, startId: Int) {
        try {
            val account = WarpManager.account(this, mode)
            val (v4, v6) = WarpManager.tunnelAddresses(account, mode)
            val builder = Builder()
                .setSession("WarpGo")
                .setMtu(1280)
                .addAddress(v4, 32)
                .addRoute("0.0.0.0", 0)
                .addRoute("::", 0)
                .addDnsServer("1.1.1.1")
                .addDnsServer("1.0.0.1")
                // WarpGo's own sockets (the tunnel itself, tests, updates) go around
                // the VPN, so the tunnel never loops into itself.
                .addDisallowedApplication(packageName)
            if (v6 != null) builder.addAddress(v6, 128)
            if (Build.VERSION.SDK_INT >= 29) builder.setMetered(false)

            val pfd = builder.establish()
                ?: throw IllegalStateException("VPN permission is missing. Tap Connect again.")
            val fd = pfd.detachFd().toLong() // the Go core owns and closes it from here
            if (mode.isMasque) {
                Warpcore.startMasque(fd, account, mode.http2)
            } else {
                Warpcore.startWireGuard(fd, account, mode.wgPort.toLong())
            }
        } catch (e: Exception) {
            WarpManager.lastError = e.message ?: e.toString()
            Warpcore.stop()
            stopSelf(startId)
        }
    }

    override fun onRevoke() {
        Warpcore.stop()
        stopSelf()
    }

    override fun onDestroy() {
        Warpcore.stop()
        super.onDestroy()
    }
}
