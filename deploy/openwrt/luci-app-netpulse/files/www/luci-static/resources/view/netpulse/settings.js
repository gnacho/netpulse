'use strict';
'require view';
'require form';

/*
 * netpulse/settings — edición de /etc/config/netpulse-agent (Fase 11).
 * "Save & Apply" hace uci commit + reload_config; el init del agente
 * declara procd_add_reload_trigger, así que el servicio se reinicia solo
 * con la config nueva — sin botones extra ni ejecuciones a mano.
 */

return view.extend({
	render: function() {
		var m, s, o;

		m = new form.Map('netpulse-agent', _('NetPulse agent'),
			_('Connection of the local agent to the NetPulse server. ' +
			  'Saving applies the change and restarts the agent automatically.'));

		s = m.section(form.NamedSection, 'main', 'main', _('Connection'));
		s.addremove = false;

		o = s.option(form.Value, 'server', _('Server URL'),
			_('Base URL of the NetPulse server, e.g. http://192.168.1.226:3000.'));
	o.placeholder = 'http://192.168.1.226:3000';
	o.rmempty = false;
	o.datatype = 'url';

		o = s.option(form.Value, 'slug', _('Slug'),
			_('Identifier of this router in NetPulse (as created in Settings → Agents).'));
		o.rmempty = false;

		o = s.option(form.Value, 'token', _('Token'),
			_('Per-device token (64 hex). Shown once when the agent is created on the server.'));
		o.password = true;
		o.rmempty = false;

		o = s.option(form.Value, 'interval', _('Push interval'),
			_('Full probe cycle. Plain seconds or a Go duration ("30", "15s", "1m").'));
		o.placeholder = '30s';

		o = s.option(form.Value, 'wan_target', _('WAN ping target'),
			_('Gateway only: public IP probed for WAN loss/latency (e.g. 1.1.1.1). Leave empty on access points.'));
		o.datatype = 'host';
		o.rmempty = true;

		o = s.option(form.Value, 'gw_target', _('Gateway ping target'),
			_('Access points only: LAN IP of the gateway for the short latency probe. Leave empty on the gateway.'));
		o.datatype = 'host';
		o.rmempty = true;

		o = s.option(form.Flag, 'insecure_tls', _('Accept self-signed TLS'),
			_('Skips server certificate verification. Only for LANs without a CA; scheduled for removal once fingerprint pinning lands (Fase 9).'));
		o.rmempty = true;

		return m.render();
	}
});
