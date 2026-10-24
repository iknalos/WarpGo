package com.iknalos.warpgo

/** The ways WarpGo can reach Cloudflare. [id] is also what the Go core reports in Stats(). */
enum class Mode(val id: String, val label: String, val wgPort: Int = 0, val http2: Boolean = false) {
    WG_4500("wireguard-4500", "WireGuard · port 4500", wgPort = 4500),
    WG_2408("wireguard-2408", "WireGuard · port 2408", wgPort = 2408),
    WG_500("wireguard-500", "WireGuard · port 500", wgPort = 500),
    MASQUE_H3("masque-h3", "MASQUE · HTTP/3"),
    MASQUE_H2("masque-h2", "MASQUE · HTTP/2", http2 = true);

    val isMasque get() = wgPort == 0

    companion object {
        fun fromId(id: String?): Mode? = entries.firstOrNull { it.id == id }

        /**
         * Auto tries what worked last time on this network first, then WireGuard on
         * 4500 (what WarpGo always used), then the other WireGuard ports, then MASQUE.
         * Networks where WireGuard works keep using WireGuard.
         */
        fun autoOrder(remembered: Mode?): List<Mode> {
            val base = listOf(WG_4500, WG_2408, WG_500, MASQUE_H3, MASQUE_H2)
            return if (remembered == null) base else listOf(remembered) + (base - remembered)
        }
    }
}
