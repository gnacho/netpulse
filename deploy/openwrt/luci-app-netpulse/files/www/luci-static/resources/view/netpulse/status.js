'use strict';
'require view';
'require rpc';
'require poll';
'require uci';
'require ui';

/*
 * netpulse/status — vista de nodo (Fase 11): estado LOCAL del agente en
 * ESTE router, leído del plugin rpcd luci.netpulse (procd + /proc +
 * heartbeat + logread). Complementa a la webapp sin duplicarla: el puente
 * "Open NetPulse" usa la URL del servidor ya configurada en UCI.
 */

var callStatus = rpc.declare({
	object: 'luci.netpulse',
	method: 'status',
	expect: { }
});

var callLogs = rpc.declare({
	object: 'luci.netpulse',
	method: 'logs',
	params: [ 'lines' ],
	expect: { }
});

var callRestart = rpc.declare({
	object: 'luci.netpulse',
	method: 'restart',
	expect: { }
});

var callTest = rpc.declare({
	object: 'luci.netpulse',
	method: 'test',
	expect: { }
});

var LOG_LINES = 80;

function fmtDuration(s) {
	if (s == null || s < 0)
		return '—';
	if (s < 60)
		return '%ds'.format(s);
	if (s < 3600)
		return '%dm %ds'.format(Math.floor(s / 60), s % 60);
	if (s < 86400)
		return '%dh %dm'.format(Math.floor(s / 3600), Math.floor((s % 3600) / 60));
	return '%dd %dh'.format(Math.floor(s / 86400), Math.floor((s % 86400) / 3600));
}

function fmtRss(kb) {
	if (kb == null)
		return '—';
	return '%.1f MiB'.format(kb / 1024);
}

function badge(text, kind) {
	/* kind: ok | warn | err — colores propios para no depender de clases
	 * concretas del tema activo. */
	var colors = { ok: '#2e7d32', warn: '#b26a00', err: '#c62828' };
	return E('span', {
		'style': 'display:inline-block;padding:1px 8px;border-radius:9px;' +
			'color:#fff;font-size:90%;background:' + (colors[kind] || '#666')
	}, text);
}

function serviceBadge(st) {
	if (!st.installed)
		return badge(_('Not installed'), 'err');
	if (!st.running)
		return badge(_('Stopped'), 'err');
	return badge(_('Running (PID %d)').format(st.pid), 'ok');
}

function heartbeatBadge(st) {
	if (st.heartbeat_age_s == null)
		return badge(_('No heartbeat yet'), 'warn');
	/* El agente late tras cada ciclo de push (30 s por defecto); el
	 * watchdog del router actúa a los 300 s. Umbral visual intermedio. */
	if (st.heartbeat_age_s <= 90)
		return badge(_('Pushing (%s ago)').format(fmtDuration(st.heartbeat_age_s)), 'ok');
	return badge(_('Stalled (%s ago)').format(fmtDuration(st.heartbeat_age_s)), 'warn');
}

function updateView(st, logs) {
	var set = function(id, node) {
		var el = document.getElementById(id);
		if (!el)
			return;
		while (el.firstChild)
			el.removeChild(el.firstChild);
		el.appendChild(typeof node == 'string' ? document.createTextNode(node) : node);
	};

	set('np-service', serviceBadge(st));
	set('np-heartbeat', heartbeatBadge(st));
	set('np-version', st.version || '—');
	set('np-enabled', st.enabled ? _('Yes') : _('No'));
	set('np-rss', fmtRss(st.rss_kb));
	set('np-uptime', fmtDuration(st.uptime_s));

	var pre = document.getElementById('np-logs');
	if (pre) {
		var atBottom = (pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 8);
		pre.textContent = logs || _('(no log lines matched)');
		if (atBottom)
			pre.scrollTop = pre.scrollHeight;
	}
}

function handleRestart() {
	ui.showModal(_('Restarting'), [
		E('p', { 'class': 'cbi-modal-text' }, _('Restarting the NetPulse agent…'))
	]);
	return callRestart().then(function(res) {
		ui.hideModal();
		if (res && res.ok)
			ui.addNotification(null, E('p', _('NetPulse agent restarted.')), 'info');
		else
			ui.addNotification(null, E('p', _('Restart failed (exit code %s).').format(res ? res.code : '?')), 'error');
	}).catch(function(err) {
		ui.hideModal();
		ui.addNotification(null, E('p', _('Restart failed: %s').format(err.message)), 'error');
	});
}

function handleTest() {
	ui.showModal(_('Testing'), [
		E('p', { 'class': 'cbi-modal-text' }, _('Testing connection to the server…'))
	]);
	return callTest().then(function(res) {
		ui.hideModal();
		if (res && res.ok)
			ui.addNotification(null, E('p', _('Connection OK (HTTP 200).')), 'info');
		else
			ui.addNotification(null, E('p', _('Connection failed (%s).').format(res ? (res.code || res.error || 'unknown') : 'unknown')), 'error');
	}).catch(function(err) {
		ui.hideModal();
		ui.addNotification(null, E('p', _('Test failed: %s').format(err.message)), 'error');
	});
}

function row(label, id, initial) {
	return E('tr', { 'class': 'tr' }, [
		E('td', { 'class': 'td left', 'width': '33%' }, label),
		E('td', { 'class': 'td left', 'id': id }, [ initial != null ? initial : '—' ])
	]);
}

return view.extend({
	handleSave: null,
	handleSaveApply: null,
	handleReset: null,

	load: function() {
		return Promise.all([
			uci.load('netpulse-agent'),
			callStatus().catch(function() { return {}; }),
			callLogs(LOG_LINES).catch(function() { return {}; })
		]);
	},

	render: function(data) {
		var st = data[1] || {};
		var logs = (data[2] && data[2].lines) || '';
		var server = uci.get('netpulse-agent', 'main', 'server') || '';
		var slug = uci.get('netpulse-agent', 'main', 'slug') || '';

		var buttons = [
			E('button', {
				'class': 'btn cbi-button cbi-button-action',
				'click': function() { handleRestart(); }
			}, _('Restart agent')),
			E('button', {
				'class': 'btn cbi-button cbi-button-neutral',
				'click': function() { handleTest(); }
			}, _('Test connection'))
		];
		if (server != '') {
			buttons.push(' ');
			buttons.push(E('a', {
				'class': 'btn cbi-button cbi-button-apply',
				'href': server,
				'target': '_blank',
				'rel': 'noopener'
			}, _('Open NetPulse ↗')));
		}

		var root = E('div', { 'class': 'cbi-map' }, [
			E('h2', {}, _('NetPulse')),
			E('div', { 'class': 'cbi-map-descr' },
				slug != ''
					? _('Local state of the NetPulse agent on this router (slug “%s”). The network-wide view lives in the NetPulse web app.').format(slug)
					: _('Local state of the NetPulse agent on this router. The network-wide view lives in the NetPulse web app.')),

			E('div', { 'class': 'cbi-section' }, [
				E('table', { 'class': 'table' }, [
					row(_('Service'), 'np-service', serviceBadge(st)),
					row(_('Heartbeat'), 'np-heartbeat', heartbeatBadge(st)),
					row(_('Agent version'), 'np-version', st.version || '—'),
					row(_('Start on boot'), 'np-enabled', st.enabled ? _('Yes') : _('No')),
					row(_('Memory (RSS)'), 'np-rss', fmtRss(st.rss_kb)),
					row(_('Process uptime'), 'np-uptime', fmtDuration(st.uptime_s))
				]),
				E('div', { 'style': 'margin-top:.5em' }, buttons)
			]),

			E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, _('Recent log')),
				E('pre', {
					'id': 'np-logs',
					'style': 'height:20em;overflow:auto;font-size:90%;' +
						'white-space:pre-wrap;word-break:break-all'
				}, [ logs || _('(no log lines matched)') ])
			])
		]);

		poll.add(function() {
			return Promise.all([
				callStatus().catch(function() { return {}; }),
				callLogs(LOG_LINES).catch(function() { return {}; })
			]).then(function(r) {
				updateView(r[0] || {}, (r[1] && r[1].lines) || '');
			});
		}, 5);

		return root;
	}
});
