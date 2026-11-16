export interface PeerHistoryPoint {
    timestamp: number;
    rx: number;
    tx: number;
    lastHandshake: number;
}

export interface PeerHistory {
    publicKey: string;
    points: PeerHistoryPoint[];
}

export interface InterfaceHistory {
    interface: string;
    peers: PeerHistory[];
}
