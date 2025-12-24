package dashboard

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Arbtribot Dashboard</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: #333;
            min-height: 100vh;
            padding: 20px;
        }

        .container {
            max-width: 1400px;
            margin: 0 auto;
        }

        .header {
            background: white;
            border-radius: 12px;
            padding: 24px;
            margin-bottom: 20px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
        }

        .header h1 {
            color: #667eea;
            margin-bottom: 12px;
        }

        .status-bar {
            display: flex;
            gap: 20px;
            flex-wrap: wrap;
            margin-top: 16px;
        }

        .status-item {
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .status-badge {
            padding: 4px 12px;
            border-radius: 20px;
            font-size: 12px;
            font-weight: 600;
            text-transform: uppercase;
        }

        .badge-running {
            background: #10b981;
            color: white;
        }

        .badge-simulation {
            background: #f59e0b;
            color: white;
        }

        .badge-live {
            background: #ef4444;
            color: white;
        }

        .grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 20px;
            margin-bottom: 20px;
        }

        .card {
            background: white;
            border-radius: 12px;
            padding: 24px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
        }

        .card h2 {
            color: #667eea;
            margin-bottom: 16px;
            font-size: 20px;
        }

        .stat {
            display: flex;
            justify-content: space-between;
            padding: 12px 0;
            border-bottom: 1px solid #e5e7eb;
        }

        .stat:last-child {
            border-bottom: none;
        }

        .stat-label {
            color: #6b7280;
            font-weight: 500;
        }

        .stat-value {
            color: #111827;
            font-weight: 600;
        }

        .stat-value.positive {
            color: #10b981;
        }

        .stat-value.negative {
            color: #ef4444;
        }

        .table-container {
            overflow-x: auto;
        }

        table {
            width: 100%;
            border-collapse: collapse;
        }

        th, td {
            padding: 12px;
            text-align: left;
            border-bottom: 1px solid #e5e7eb;
        }

        th {
            background: #f9fafb;
            color: #374151;
            font-weight: 600;
            font-size: 12px;
            text-transform: uppercase;
        }

        td {
            color: #111827;
            font-size: 14px;
        }

        tr:hover {
            background: #f9fafb;
        }

        .side-long {
            color: #10b981;
            font-weight: 600;
        }

        .side-short {
            color: #ef4444;
            font-weight: 600;
        }

        .refresh-indicator {
            display: inline-block;
            width: 12px;
            height: 12px;
            border-radius: 50%;
            background: #10b981;
            margin-right: 8px;
            animation: pulse 2s infinite;
        }

        @keyframes pulse {
            0%, 100% {
                opacity: 1;
            }
            50% {
                opacity: 0.5;
            }
        }

        .full-width {
            grid-column: 1 / -1;
        }

        .config-section {
            margin-top: 16px;
        }

        .config-item {
            display: grid;
            grid-template-columns: 200px 1fr;
            gap: 12px;
            padding: 8px 0;
            border-bottom: 1px solid #e5e7eb;
        }

        .config-item:last-child {
            border-bottom: none;
        }

        .config-label {
            color: #6b7280;
            font-weight: 500;
        }

        .config-value {
            color: #111827;
            font-family: 'Courier New', monospace;
            font-size: 13px;
        }

        .loading {
            text-align: center;
            padding: 40px;
            color: #6b7280;
        }

        .error {
            background: #fee2e2;
            color: #991b1b;
            padding: 12px;
            border-radius: 8px;
            margin: 12px 0;
        }

        .btn-close {
            background: #ef4444;
            color: white;
            border: none;
            padding: 6px 12px;
            border-radius: 6px;
            cursor: pointer;
            font-size: 12px;
            font-weight: 600;
            transition: background 0.2s;
        }

        .btn-close:hover {
            background: #dc2626;
        }

        .btn-close:disabled {
            background: #9ca3af;
            cursor: not-allowed;
        }

        .btn-close:active {
            background: #b91c1c;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🤖 Arbtribot Dashboard</h1>
            <div class="status-bar">
                <div class="status-item">
                    <span class="status-badge badge-running">Running</span>
                    <span id="uptime">-</span>
                </div>
                <div class="status-item">
                    <span class="status-badge" id="mode-badge">-</span>
                    <span id="trading-mode">-</span>
                </div>
                <div class="status-item">
                    <span class="status-badge" id="sim-badge">-</span>
                    <span id="sim-mode">-</span>
                </div>
                <div class="status-item">
                    <span class="refresh-indicator"></span>
                    <span id="last-update">Updating...</span>
                </div>
            </div>
        </div>

        <div class="grid">
            <div class="card">
                <h2>📊 Statistics</h2>
                <div id="stats-content">
                    <div class="loading">Loading statistics...</div>
                </div>
            </div>

            <div class="card">
                <h2>⚙️ Configuration</h2>
                <div id="config-content">
                    <div class="loading">Loading configuration...</div>
                </div>
            </div>
        </div>

        <div class="card full-width">
            <h2>📈 Open Positions</h2>
            <div class="table-container">
                <div id="positions-content">
                    <div class="loading">Loading positions...</div>
                </div>
            </div>
        </div>

        <div class="card full-width">
            <h2>📋 Recent Trades</h2>
            <div class="table-container">
                <div id="trades-content">
                    <div class="loading">Loading recent trades...</div>
                </div>
            </div>
        </div>
    </div>

    <script>
        let updateInterval;

        async function fetchAPI(endpoint) {
            try {
                const response = await fetch('/api/' + endpoint);
                if (!response.ok) throw new Error('Network response was not ok');
                return await response.json();
            } catch (error) {
                console.error('Error fetching ' + endpoint + ':', error);
                return null;
            }
        }

        function formatNumber(num, decimals = 2) {
            if (num === null || num === undefined) return '-';
            const n = parseFloat(num);
            if (isNaN(n)) return num;
            return n.toFixed(decimals);
        }

        function formatPercent(num) {
            if (num === null || num === undefined) return '-';
            const n = parseFloat(num);
            if (isNaN(n)) return num;
            return (n >= 0 ? '+' : '') + n.toFixed(4) + '%';
        }

        function formatTime(isoString) {
            if (!isoString) return '-';
            const date = new Date(isoString);
            return date.toLocaleString();
        }

        function getPnLClass(value) {
            const num = parseFloat(value);
            if (isNaN(num)) return '';
            return num >= 0 ? 'positive' : 'negative';
        }

        async function updateStatus() {
            const status = await fetchAPI('status');
            if (!status) return;

            document.getElementById('uptime').textContent = 'Uptime: ' + status.uptime;
            document.getElementById('trading-mode').textContent = 'Mode: ' + (status.tradingMode || 'N/A');
            document.getElementById('mode-badge').textContent = status.tradingMode || 'N/A';
            document.getElementById('mode-badge').className = 'status-badge badge-running';

            const simMode = status.simulationMode ? 'Simulation' : 'Live';
            document.getElementById('sim-mode').textContent = simMode;
            const simBadge = document.getElementById('sim-badge');
            simBadge.textContent = simMode;
            simBadge.className = 'status-badge ' + (status.simulationMode ? 'badge-simulation' : 'badge-live');

            document.getElementById('last-update').textContent = 'Updated: ' + new Date().toLocaleTimeString();
        }

        async function updateStats() {
            const stats = await fetchAPI('stats');
            if (!stats) {
                document.getElementById('stats-content').innerHTML = '<div class="error">Failed to load statistics</div>';
                return;
            }

            let html = '';
            if (stats.totalTrades !== undefined) {
                html += '<div class="stat"><span class="stat-label">Total Trades</span><span class="stat-value">' + stats.totalTrades + '</span></div>';
                html += '<div class="stat"><span class="stat-label">Winning Trades</span><span class="stat-value positive">' + (stats.winningTrades || 0) + '</span></div>';
                html += '<div class="stat"><span class="stat-label">Losing Trades</span><span class="stat-value negative">' + (stats.losingTrades || 0) + '</span></div>';
                html += '<div class="stat"><span class="stat-label">Win Rate</span><span class="stat-value">' + 
                    (stats.totalTrades > 0 ? formatPercent((stats.winningTrades / stats.totalTrades) * 100) : '0%') + '</span></div>';
                html += '<div class="stat"><span class="stat-label">Total P&L</span><span class="stat-value ' + getPnLClass(stats.totalPnL) + '">' + formatNumber(stats.totalPnL, 4) + ' USDT</span></div>';
                html += '<div class="stat"><span class="stat-label">Total P&L %</span><span class="stat-value ' + getPnLClass(stats.totalPnLPercent) + '">' + formatPercent(stats.totalPnLPercent) + '</span></div>';
                if (stats.bestTradePnL) {
                    html += '<div class="stat"><span class="stat-label">Best Trade</span><span class="stat-value positive">' + formatNumber(stats.bestTradePnL, 4) + ' USDT</span></div>';
                }
                if (stats.worstTradePnL) {
                    html += '<div class="stat"><span class="stat-label">Worst Trade</span><span class="stat-value negative">' + formatNumber(stats.worstTradePnL, 4) + ' USDT</span></div>';
                }
                if (stats.totalVolume) {
                    html += '<div class="stat"><span class="stat-label">Total Volume</span><span class="stat-value">' + formatNumber(stats.totalVolume, 2) + ' USDT</span></div>';
                }
            }
            if (stats.openPositions !== undefined) {
                html += '<div class="stat"><span class="stat-label">Open Positions</span><span class="stat-value">' + stats.openPositions + '</span></div>';
            }
            if (stats.startTime) {
                html += '<div class="stat"><span class="stat-label">Start Time</span><span class="stat-value">' + formatTime(stats.startTime) + '</span></div>';
            }
            if (stats.lastTradeTime) {
                html += '<div class="stat"><span class="stat-label">Last Trade</span><span class="stat-value">' + formatTime(stats.lastTradeTime) + '</span></div>';
            }

            document.getElementById('stats-content').innerHTML = html || '<div class="error">No statistics available</div>';
        }

        async function updateConfig() {
            const config = await fetchAPI('config');
            if (!config) {
                document.getElementById('config-content').innerHTML = '<div class="error">Failed to load configuration</div>';
                return;
            }

            let html = '<div class="config-section">';
            html += '<div class="config-item"><span class="config-label">Trading Mode:</span><span class="config-value">' + (config.tradingMode || 'N/A') + '</span></div>';
            html += '<div class="config-item"><span class="config-label">Simulation Mode:</span><span class="config-value">' + (config.simulationMode ? 'Yes' : 'No') + '</span></div>';
            html += '<div class="config-item"><span class="config-label">Fee Rate:</span><span class="config-value">' + formatPercent((config.feeRate || 0) * 100) + '</span></div>';

            if (config.marginConfig) {
                const mc = config.marginConfig;
                html += '<div class="config-item"><span class="config-label">Position Size:</span><span class="config-value">' + formatNumber(mc.positionSize, 2) + ' USDT</span></div>';
                html += '<div class="config-item"><span class="config-label">Max Positions:</span><span class="config-value">' + mc.maxPositions + '</span></div>';
                html += '<div class="config-item"><span class="config-label">Stop Loss:</span><span class="config-value">' + formatPercent(mc.stopLoss * 100) + '</span></div>';
                html += '<div class="config-item"><span class="config-label">Entry Change:</span><span class="config-value">' + formatPercent(mc.entryChange * 100) + '</span></div>';
                html += '<div class="config-item"><span class="config-label">Margin Type:</span><span class="config-value">' + mc.marginType + '</span></div>';
            }

            if (config.futuresConfig) {
                const fc = config.futuresConfig;
                html += '<div class="config-item"><span class="config-label">Position Size:</span><span class="config-value">' + formatNumber(fc.positionSize, 2) + ' USDT</span></div>';
                html += '<div class="config-item"><span class="config-label">Max Positions:</span><span class="config-value">' + fc.maxPositions + '</span></div>';
                html += '<div class="config-item"><span class="config-label">Leverage:</span><span class="config-value">' + fc.leverage + 'x</span></div>';
                html += '<div class="config-item"><span class="config-label">Stop Loss:</span><span class="config-value">' + formatPercent(fc.stopLoss * 100) + '</span></div>';
            }

            if (config.gridConfig) {
                const gc = config.gridConfig;
                html += '<div class="config-item"><span class="config-label">Profit Target:</span><span class="config-value">' + formatPercent(gc.profitTarget * 100) + '</span></div>';
                html += '<div class="config-item"><span class="config-label">Stop Loss:</span><span class="config-value">' + formatPercent(gc.stopLoss * 100) + '</span></div>';
                html += '<div class="config-item"><span class="config-label">Max Positions:</span><span class="config-value">' + gc.maxPositions + '</span></div>';
            }

            html += '</div>';
            document.getElementById('config-content').innerHTML = html;
        }

        async function updatePositions() {
            const positions = await fetchAPI('positions');
            if (!positions || positions.length === 0) {
                document.getElementById('positions-content').innerHTML = '<div class="loading">No open positions</div>';
                return;
            }

            let html = '<table><thead><tr>';
            html += '<th>Symbol</th><th>Side</th><th>Entry Price</th><th>Current Price</th>';
            html += '<th>Quantity</th><th>Unrealized P&L</th><th>Unrealized P&L %</th>';
            html += '<th>Entry Time</th><th>Hold Duration</th><th>Status</th><th>Action</th>';
            html += '</tr></thead><tbody>';

            positions.forEach(pos => {
                html += '<tr>';
                html += '<td><strong>' + pos.symbol + '</strong></td>';
                html += '<td><span class="side-' + pos.side.toLowerCase() + '">' + pos.side + '</span></td>';
                html += '<td>' + formatNumber(pos.entryPrice, 8) + '</td>';
                html += '<td>' + formatNumber(pos.currentPrice, 8) + '</td>';
                html += '<td>' + formatNumber(pos.quantity, 8) + '</td>';
                html += '<td><span class="stat-value ' + getPnLClass(pos.unrealizedPnL) + '">' + formatNumber(pos.unrealizedPnL, 4) + '</span></td>';
                html += '<td><span class="stat-value ' + getPnLClass(pos.unrealizedPnLPercent) + '">' + formatPercent(pos.unrealizedPnLPercent) + '</span></td>';
                html += '<td>' + formatTime(pos.entryTime) + '</td>';
                html += '<td>' + pos.holdDuration + '</td>';
                html += '<td>' + (pos.status || 'OPEN') + '</td>';
                html += '<td><button class="btn-close" onclick="closePosition(\'' + pos.symbol + '\', this)">Close</button></td>';
                html += '</tr>';
            });

            html += '</tbody></table>';
            document.getElementById('positions-content').innerHTML = html;
        }

        async function updateRecentTrades() {
            const trades = await fetchAPI('recent-trades');
            if (!trades || trades.length === 0) {
                document.getElementById('trades-content').innerHTML = '<div class="loading">No recent trades</div>';
                return;
            }

            let html = '<table><thead><tr>';
            html += '<th>Symbol</th><th>Side</th><th>Entry Price</th><th>Exit Price</th>';
            html += '<th>Quantity</th><th>P&L</th><th>P&L %</th>';
            html += '<th>Entry Time</th><th>Exit Time</th><th>Hold Duration</th><th>Reason</th>';
            html += '</tr></thead><tbody>';

            // Show most recent first
            trades.slice().reverse().forEach(trade => {
                html += '<tr>';
                html += '<td><strong>' + trade.symbol + '</strong></td>';
                html += '<td><span class="side-' + trade.side.toLowerCase() + '">' + trade.side + '</span></td>';
                html += '<td>' + formatNumber(trade.entryPrice, 8) + '</td>';
                html += '<td>' + formatNumber(trade.exitPrice, 8) + '</td>';
                html += '<td>' + formatNumber(trade.quantity, 8) + '</td>';
                html += '<td><span class="stat-value ' + getPnLClass(trade.pnl) + '">' + formatNumber(trade.pnl, 4) + '</span></td>';
                html += '<td><span class="stat-value ' + getPnLClass(trade.pnlPercent) + '">' + formatPercent(trade.pnlPercent) + '</span></td>';
                html += '<td>' + formatTime(trade.entryTime) + '</td>';
                html += '<td>' + formatTime(trade.exitTime) + '</td>';
                html += '<td>' + trade.holdDuration + '</td>';
                html += '<td>' + (trade.reason || '-') + '</td>';
                html += '</tr>';
            });

            html += '</tbody></table>';
            document.getElementById('trades-content').innerHTML = html;
        }

        async function closePosition(symbol, button) {
            if (!confirm('Are you sure you want to close position ' + symbol + ' at the current market price?')) {
                return;
            }

            button.disabled = true;
            button.textContent = 'Closing...';

            try {
                const response = await fetch('/api/close-position', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                    },
                    body: JSON.stringify({ symbol: symbol })
                });

                const result = await response.json();

                if (result.success) {
                    button.textContent = 'Closed';
                    button.style.background = '#10b981';
                    // Refresh positions after a short delay
                    setTimeout(() => {
                        updatePositions();
                        updateStats();
                        updateRecentTrades();
                    }, 1000);
                } else {
                    alert('Error closing position: ' + (result.error || 'Unknown error'));
                    button.disabled = false;
                    button.textContent = 'Close';
                }
            } catch (error) {
                alert('Error closing position: ' + error.message);
                button.disabled = false;
                button.textContent = 'Close';
            }
        }

        async function updateAll() {
            await Promise.all([
                updateStatus(),
                updateStats(),
                updateConfig(),
                updatePositions(),
                updateRecentTrades()
            ]);
        }

        // Initial load
        updateAll();

        // Auto-refresh every 2 seconds
        updateInterval = setInterval(updateAll, 2000);

        // Cleanup on page unload
        window.addEventListener('beforeunload', () => {
            if (updateInterval) {
                clearInterval(updateInterval);
            }
        });
    </script>
</body>
</html>`
