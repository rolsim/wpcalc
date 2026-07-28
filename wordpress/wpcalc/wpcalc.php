<?php
/**
 * Plugin Name:       wpcalc
 * Description:       Monthly employee working-hours grid, served by a bundled Go sidecar over a unix socket.
 * Version:           0.1.0
 * Requires at least: 6.4
 * Requires PHP:      8.1
 * License:           MIT
 *
 * This file is deliberately thin. It owns four things and nothing else:
 * capability checks, supervising the sidecar process, signing the identity it
 * asserts, and proxying bytes. Every line of logic that lives here is a line
 * that has to be tested twice, in two languages, so the logic lives in Go.
 */

if (!defined('ABSPATH')) {
    exit;
}

final class WPCalc_Plugin
{
    const MENU_SLUG      = 'wpcalc';
    const OPT_BINARY     = 'wpcalc_binary_path';
    const OPT_SECRET     = 'wpcalc_shared_secret';
    const PATH_PARAM     = 'wpcalc_path';
    const CAPABILITY     = 'manage_options';
    const NONCE_ACTION   = 'wpcalc_request';
    const NONCE_FIELD    = 'wpcalc_nonce';
    const START_TIMEOUT  = 5.0;
    const REQUEST_TIMEOUT = 15;

    public static function boot(): void
    {
        $self = new self();
        add_action('admin_menu', [$self, 'register_menu']);
        add_action('admin_init', [$self, 'register_settings']);
        // Assets and writes are handled before WordPress renders any admin
        // chrome, because a stylesheet or a redirect wrapped in an admin page
        // is not a stylesheet or a redirect.
        add_action('admin_init', [$self, 'maybe_handle_raw'], 5);
        add_action('admin_enqueue_scripts', [$self, 'enqueue_assets']);
        register_deactivation_hook(__FILE__, [__CLASS__, 'on_deactivate']);
    }

    // ---------------------------------------------------------------- paths

    private function runtime_dir(): string
    {
        $uploads = wp_upload_dir();
        $dir = trailingslashit($uploads['basedir']) . 'wpcalc';
        if (!is_dir($dir)) {
            wp_mkdir_p($dir);
            // The database and socket live here; nothing about them should be
            // reachable over HTTP just because uploads/ usually is.
            @file_put_contents($dir . '/.htaccess', "Require all denied\n");
            @file_put_contents($dir . '/index.php', "<?php // Silence is golden.\n");
        }
        return $dir;
    }

    private function socket_path(): string { return $this->runtime_dir() . '/wpcalc.sock'; }
    private function pid_path(): string    { return $this->runtime_dir() . '/wpcalc.pid'; }
    private function db_path(): string     { return $this->runtime_dir() . '/wpcalc.db'; }
    private function log_path(): string    { return $this->runtime_dir() . '/wpcalc.log'; }

    private function binary_path(): string
    {
        $configured = trim((string) get_option(self::OPT_BINARY, ''));
        if ($configured !== '') {
            return $configured;
        }
        return plugin_dir_path(__FILE__) . 'bin/wpcalc';
    }

    /**
     * The secret shared with the sidecar, created on first use.
     *
     * It is what lets the sidecar believe the identity this plugin asserts, so
     * it is generated rather than defaulted: a shipped default would be the
     * same on every install.
     */
    private function secret(): string
    {
        $secret = (string) get_option(self::OPT_SECRET, '');
        if (strlen($secret) < 32) {
            $secret = bin2hex(random_bytes(24));
            update_option(self::OPT_SECRET, $secret, false);
        }
        return $secret;
    }

    // ------------------------------------------------------------ lifecycle

    /** Is the sidecar answering on the socket right now? */
    private function is_healthy(): bool
    {
        $res = $this->request('GET', '/healthz', [], null, 2);
        return $res !== null && $res['status'] === 200;
    }

    /**
     * Start the sidecar unless it is already answering.
     *
     * Health is decided by asking the socket, not by checking whether a PID
     * exists: a process that is alive but wedged would pass a PID check and
     * fail every request, which is the failure mode hardest to diagnose from
     * the admin screen.
     */
    private function ensure_running(): ?string
    {
        if ($this->is_healthy()) {
            return null;
        }
        if (!function_exists('proc_open')) {
            return 'proc_open() is disabled on this host, so the wpcalc service cannot be started. '
                 . 'Ask your hosting provider to allow it, or run wpcalc as a system service and point the plugin at its socket.';
        }

        $binary = $this->binary_path();
        if (!is_file($binary)) {
            return sprintf('The wpcalc binary was not found at %s. Set the correct path in the wpcalc settings.', esc_html($binary));
        }
        if (!is_executable($binary)) {
            return sprintf('The wpcalc binary at %s is not executable (chmod +x).', esc_html($binary));
        }

        $env = [
            'WPCALC_SECRET'     => $this->secret(),
            'WPCALC_BASE_PATH'  => admin_url('admin.php?page=' . self::MENU_SLUG),
            'WPCALC_LINK_PARAM' => self::PATH_PARAM,
        ];
        $prefix = '';
        foreach ($env as $k => $v) {
            $prefix .= $k . '=' . escapeshellarg($v) . ' ';
        }

        // The sidecar is backgrounded by a throwaway shell that exits at once,
        // and the PID comes back on that shell's stdout.
        //
        // proc_open alone is not enough: proc_close() waits for the process to
        // terminate, and this one is meant to run forever, so closing the
        // handle would hang the admin request until the browser gave up. Nor
        // can the handle simply be leaked — PHP closes it at request shutdown
        // with the same wait. Detaching through a shell is what lets the
        // sidecar outlive the PHP worker that started it.
        // Environment assignments must precede the command, and nohup *is* the
        // command: "nohup VAR=x prog" makes nohup look for a program literally
        // named "VAR=x".
        $inner = sprintf(
            '%snohup %s serve --socket %s --db %s >>%s 2>&1 & echo $!',
            $prefix,
            escapeshellarg($binary),
            escapeshellarg($this->socket_path()),
            escapeshellarg($this->db_path()),
            escapeshellarg($this->log_path())
        );

        $descriptors = [
            0 => ['file', '/dev/null', 'r'],
            1 => ['pipe', 'w'],
            2 => ['pipe', 'w'],
        ];

        $proc = @proc_open(['/bin/sh', '-c', $inner], $descriptors, $pipes, $this->runtime_dir());
        if (!is_resource($proc)) {
            return 'Failed to start the wpcalc service. See ' . esc_html($this->log_path()) . ' for details.';
        }

        $pid = (int) trim((string) stream_get_contents($pipes[1]));
        foreach ($pipes as $pipe) {
            if (is_resource($pipe)) {
                fclose($pipe);
            }
        }
        // Returns immediately: the shell has already exited.
        proc_close($proc);

        if ($pid > 0) {
            @file_put_contents($this->pid_path(), (string) $pid);
        }

        $deadline = microtime(true) + self::START_TIMEOUT;
        while (microtime(true) < $deadline) {
            if ($this->is_healthy()) {
                return null;
            }
            usleep(100000);
        }

        return 'The wpcalc service did not become ready within ' . self::START_TIMEOUT . ' seconds. '
             . 'The last lines of ' . esc_html($this->log_path()) . ' will say why.';
    }

    /**
     * Stop the sidecar, if one is recorded.
     *
     * SIGTERM rather than SIGKILL so it closes the database cleanly; killing
     * it outright leaves a WAL behind for the next start to recover.
     */
    private function stop_sidecar(): void
    {
        $pidFile = $this->pid_path();
        if (is_file($pidFile)) {
            $pid = (int) trim((string) @file_get_contents($pidFile));
            if ($pid > 0 && function_exists('posix_kill')) {
                @posix_kill($pid, defined('SIGTERM') ? SIGTERM : 15);
            }
            @unlink($pidFile);
        }
        @unlink($this->socket_path());
    }

    public static function on_deactivate(): void
    {
        (new self())->stop_sidecar();
    }

    // ---------------------------------------------------------------- proxy

    /**
     * Send one request to the sidecar over the unix socket.
     *
     * @return array{status:int,headers:array<string,string>,body:string}|null
     */
    private function request(string $method, string $path, array $headers = [], ?string $body = null, ?int $timeout = null)
    {
        if (!function_exists('curl_init')) {
            return null;
        }

        $ch = curl_init();
        curl_setopt_array($ch, [
            CURLOPT_UNIX_SOCKET_PATH => $this->socket_path(),
            // The host is arbitrary: the socket decides where this goes.
            CURLOPT_URL              => 'http://localhost' . $path,
            CURLOPT_CUSTOMREQUEST    => $method,
            CURLOPT_RETURNTRANSFER   => true,
            CURLOPT_HEADER           => true,
            CURLOPT_TIMEOUT          => $timeout ?? self::REQUEST_TIMEOUT,
            CURLOPT_CONNECTTIMEOUT   => 2,
            // Redirects are the application's answer and belong to the browser.
            CURLOPT_FOLLOWLOCATION   => false,
            CURLOPT_HTTPHEADER       => $this->flatten_headers($headers),
        ]);
        if ($body !== null) {
            curl_setopt($ch, CURLOPT_POSTFIELDS, $body);
        }

        $raw = curl_exec($ch);
        if ($raw === false) {
            curl_close($ch);
            return null;
        }
        $status     = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
        $headerSize = (int) curl_getinfo($ch, CURLINFO_HEADER_SIZE);
        curl_close($ch);

        return [
            'status'  => $status,
            'headers' => $this->parse_headers(substr($raw, 0, $headerSize)),
            'body'    => substr($raw, $headerSize),
        ];
    }

    /**
     * Headers asserting who the caller is, signed so the sidecar can tell they
     * came from this plugin and not from anything else that reached the socket.
     */
    private function identity_headers(): array
    {
        $user  = wp_get_current_user();
        $name  = $user && $user->exists() ? $user->user_login : '';
        $roles = $user && $user->exists() ? implode(',', (array) $user->roles) : '';
        $ts    = (string) time();

        // The separator cannot appear in a username or role, so no two distinct
        // field sets can produce the same signed message.
        $sig = hash_hmac('sha256', $name . "\n" . $roles . "\n" . $ts, $this->secret());

        return [
            'X-Wpcalc-User'      => $name,
            'X-Wpcalc-Roles'     => $roles,
            'X-Wpcalc-Timestamp' => $ts,
            'X-Wpcalc-Signature' => $sig,
        ];
    }

    /** The application path this request is addressing. */
    private function app_path(): string
    {
        $raw = isset($_REQUEST[self::PATH_PARAM]) ? wp_unslash($_REQUEST[self::PATH_PARAM]) : '/';
        if (!is_string($raw) || $raw === '') {
            $raw = '/';
        }
        // Only ever a path: a caller must not be able to redirect the proxy at
        // another host by supplying "//evil.example/".
        $path = '/' . ltrim(parse_url($raw, PHP_URL_PATH) ?: '/', '/');
        if (str_contains($path, '..')) {
            $path = '/';
        }
        $query = parse_url($raw, PHP_URL_QUERY);
        return $query ? $path . '?' . $query : $path;
    }

    /**
     * Proxy, following redirects internally.
     *
     * Used for the page render only. By the time the menu callback runs,
     * WordPress has already emitted the admin header, so a Location header
     * cannot be sent — the redirect has to be resolved here or the page shows
     * a redirect's empty body. Writes and downloads go through the raw path
     * instead, where the browser gets the real redirect.
     */
    private function proxy_following(string $path, bool $fragment): ?array
    {
        for ($hop = 0; $hop < 5; $hop++) {
            $res = $this->proxy($path, $fragment);
            if ($res === null) {
                return null;
            }
            if ($res['status'] < 300 || $res['status'] >= 400 || !isset($res['headers']['location'])) {
                return $res;
            }

            $next = $this->path_from_admin_url($res['headers']['location']);
            if ($next === null || $next === $path) {
                return $res; // not one of ours, or a loop
            }
            $path = $next;
        }
        return null;
    }

    /** Pull the application path back out of an admin URL the app generated. */
    private function path_from_admin_url(string $location): ?string
    {
        $query = parse_url($location, PHP_URL_QUERY);
        if (!$query) {
            return null;
        }
        parse_str($query, $params);
        if (empty($params[self::PATH_PARAM]) || !is_string($params[self::PATH_PARAM])) {
            return null;
        }
        return '/' . ltrim($params[self::PATH_PARAM], '/');
    }

    private function proxy(string $path, bool $fragment): ?array
    {
        $headers = $this->identity_headers();
        if ($fragment) {
            $headers['X-Wpcalc-Fragment'] = '1';
        }
        // WordPress keeps a per-user locale of its own, and it is the more
        // specific statement than the browser's header. Sending it as
        // Accept-Language lets the sidecar honour it through the negotiation it
        // already does, with no second preference stored on this side to
        // disagree with WordPress about.
        $locale = function_exists('get_user_locale') ? get_user_locale() : '';
        if ($locale !== '') {
            $headers['Accept-Language'] = str_replace('_', '-', $locale);
        } elseif (isset($_SERVER['HTTP_ACCEPT_LANGUAGE'])) {
            $headers['Accept-Language'] = sanitize_text_field(wp_unslash($_SERVER['HTTP_ACCEPT_LANGUAGE']));
        }

        $method = isset($_SERVER['REQUEST_METHOD']) ? strtoupper(sanitize_text_field(wp_unslash($_SERVER['REQUEST_METHOD']))) : 'GET';
        $body   = null;
        if ($method === 'POST') {
            $body = file_get_contents('php://input');
            if ($body === false || $body === '') {
                $body = http_build_query(wp_unslash($_POST));
            }
            $headers['Content-Type'] = isset($_SERVER['CONTENT_TYPE'])
                ? sanitize_text_field(wp_unslash($_SERVER['CONTENT_TYPE']))
                : 'application/x-www-form-urlencoded';
            if (isset($_SERVER['HTTP_X_REQUESTED_WITH'])) {
                $headers['X-Requested-With'] = sanitize_text_field(wp_unslash($_SERVER['HTTP_X_REQUESTED_WITH']));
            }
        }

        return $this->request($method, $path, $headers, $body);
    }

    // -------------------------------------------------------------- routing

    /**
     * True when this request is for our admin page.
     *
     * Reads $_REQUEST rather than $_GET: a form whose action carries the query
     * string puts it in $_GET, but a client that posts the same fields in the
     * body would otherwise slip past this check and reach no handler at all,
     * which looks like a silent success.
     */
    private function is_our_page(): bool
    {
        return isset($_REQUEST['page']) && $_REQUEST['page'] === self::MENU_SLUG;
    }

    /**
     * Handle everything that must not be wrapped in admin chrome: assets,
     * form posts, XHR, and PDF downloads.
     */
    public function maybe_handle_raw(): void
    {
        if (!$this->is_our_page()) {
            return;
        }

        $path   = $this->app_path();
        $method = isset($_SERVER['REQUEST_METHOD']) ? strtoupper(sanitize_text_field(wp_unslash($_SERVER['REQUEST_METHOD']))) : 'GET';
        $isAsset  = str_starts_with($path, '/static/');
        $isReport = str_starts_with($path, '/report/');
        $isWrite  = $method !== 'GET';

        if (!$isAsset && !$isReport && !$isWrite) {
            return; // an ordinary page render; the menu callback handles it
        }

        if (!current_user_can(self::CAPABILITY)) {
            wp_die(esc_html__('You do not have permission to use wpcalc.', 'wpcalc'), '', ['response' => 403]);
        }

        // Writes carry a nonce. Assets and report downloads are reads and do
        // not, because a stylesheet request cannot change anything and a nonce
        // on it would only break browser caching.
        if ($isWrite) {
            $nonce = isset($_REQUEST[self::NONCE_FIELD]) ? sanitize_text_field(wp_unslash($_REQUEST[self::NONCE_FIELD])) : '';
            if (!wp_verify_nonce($nonce, self::NONCE_ACTION)) {
                status_header(403);
                nocache_headers();
                echo esc_html__('Security check failed. Reload the page and try again.', 'wpcalc');
                exit;
            }
        }

        if ($err = $this->ensure_running()) {
            status_header(503);
            echo esc_html($err);
            exit;
        }

        $res = $this->proxy($path, false);
        if ($res === null) {
            status_header(502);
            echo esc_html__('The wpcalc service is not responding.', 'wpcalc');
            exit;
        }

        status_header($res['status']);
        foreach (['Content-Type', 'Content-Length', 'Content-Disposition', 'Cache-Control'] as $h) {
            if (isset($res['headers'][strtolower($h)])) {
                header($h . ': ' . $res['headers'][strtolower($h)]);
            }
        }
        // A redirect from the app names an admin URL already, because the
        // sidecar was told its base URL and link parameter at startup.
        if (isset($res['headers']['location'])) {
            header('Location: ' . $res['headers']['location']);
        }
        echo $res['body']; // phpcs:ignore WordPress.Security.EscapeOutput -- proxied response, escaped at its source
        exit;
    }

    public function enqueue_assets(string $hook): void
    {
        if (!$this->is_our_page()) {
            return;
        }
        $base = admin_url('admin.php?page=' . self::MENU_SLUG);
        wp_enqueue_style('wpcalc', $base . '&' . self::PATH_PARAM . '=' . rawurlencode('/static/app.css'), [], '0.1.0');
        wp_enqueue_script('wpcalc', $base . '&' . self::PATH_PARAM . '=' . rawurlencode('/static/app.js'), [], '0.1.0', true);
    }

    public function register_menu(): void
    {
        add_menu_page(
            __('Working hours', 'wpcalc'),
            __('Working hours', 'wpcalc'),
            self::CAPABILITY,
            self::MENU_SLUG,
            [$this, 'render_page'],
            'dashicons-calendar-alt',
            30
        );

        add_submenu_page(
            self::MENU_SLUG,
            __('wpcalc settings', 'wpcalc'),
            __('Settings', 'wpcalc'),
            self::CAPABILITY,
            self::MENU_SLUG . '-settings',
            [$this, 'render_settings']
        );
    }

    public function render_page(): void
    {
        if (!current_user_can(self::CAPABILITY)) {
            wp_die(esc_html__('You do not have permission to use wpcalc.', 'wpcalc'), '', ['response' => 403]);
        }

        echo '<div class="wrap" id="wpcalc-root">';

        if ($err = $this->ensure_running()) {
            // Degrade with an explanation. A white screen or a silent empty
            // page here is the difference between a five-minute fix and an
            // afternoon, because nothing else in WordPress will mention it.
            printf(
                '<div class="notice notice-error"><p><strong>%s</strong></p><p>%s</p></div>',
                esc_html__('wpcalc could not start', 'wpcalc'),
                esc_html($err)
            );
            echo '</div>';
            return;
        }

        $res = $this->proxy_following($this->app_path(), true);
        if ($res === null) {
            printf(
                '<div class="notice notice-error"><p>%s</p></div></div>',
                esc_html__('The wpcalc service is not responding.', 'wpcalc')
            );
            return;
        }

        // The sidecar returns a fragment here, so it drops into the admin page
        // without a nested <html> document fighting WordPress for the head.
        echo $res['body']; // phpcs:ignore WordPress.Security.EscapeOutput -- proxied fragment, escaped by html/template at its source

        // Every form the fragment contains posts back through this plugin, so
        // each needs the nonce WordPress expects. Adding it here keeps the Go
        // templates free of WordPress-specific markup.
        printf(
            '<script>(function(){var n=%s;document.querySelectorAll("#wpcalc-root form").forEach(function(f){if(f.querySelector("[name=%s]"))return;var i=document.createElement("input");i.type="hidden";i.name=%s;i.value=n;f.appendChild(i);});})();</script>',
            wp_json_encode(wp_create_nonce(self::NONCE_ACTION)),
            esc_js(self::NONCE_FIELD),
            wp_json_encode(self::NONCE_FIELD)
        );

        echo '</div>';
    }

    // ------------------------------------------------------------- settings

    public function register_settings(): void
    {
        register_setting(self::MENU_SLUG, self::OPT_BINARY, [
            'type'              => 'string',
            'sanitize_callback' => 'sanitize_text_field',
            'default'           => '',
        ]);
    }

    public function render_settings(): void
    {
        if (!current_user_can(self::CAPABILITY)) {
            wp_die(esc_html__('You do not have permission to use wpcalc.', 'wpcalc'), '', ['response' => 403]);
        }

        $notice = '';
        if (!empty($_POST['wpcalc_action'])) {
            $nonce = isset($_POST[self::NONCE_FIELD]) ? sanitize_text_field(wp_unslash($_POST[self::NONCE_FIELD])) : '';
            if (!wp_verify_nonce($nonce, self::NONCE_ACTION)) {
                wp_die(esc_html__('Security check failed.', 'wpcalc'), '', ['response' => 403]);
            }

            switch (sanitize_text_field(wp_unslash($_POST['wpcalc_action']))) {
                case 'save':
                    $path = isset($_POST[self::OPT_BINARY]) ? sanitize_text_field(wp_unslash($_POST[self::OPT_BINARY])) : '';
                    update_option(self::OPT_BINARY, $path, false);
                    // The running sidecar was started from the old path, so it
                    // has to go or the new setting silently does nothing.
                    $this->stop_sidecar();
                    $notice = __('Saved. The service will restart on the next page load.', 'wpcalc');
                    break;

                case 'restart':
                    $this->stop_sidecar();
                    $notice = __('The service was stopped and will restart on the next page load.', 'wpcalc');
                    break;

                case 'rotate':
                    // The sidecar holds the old secret in its environment, so
                    // rotating without restarting would make every request fail
                    // to authenticate.
                    delete_option(self::OPT_SECRET);
                    $this->secret();
                    $this->stop_sidecar();
                    $notice = __('A new shared secret was generated and the service was restarted.', 'wpcalc');
                    break;
            }
        }

        $binary  = $this->binary_path();
        $running = $this->is_healthy();

        echo '<div class="wrap"><h1>' . esc_html__('wpcalc settings', 'wpcalc') . '</h1>';

        if ($notice !== '') {
            printf('<div class="notice notice-success is-dismissible"><p>%s</p></div>', esc_html($notice));
        }

        // Status first: when something is wrong this is the screen someone
        // lands on, and the answer should be visible without reading a log.
        echo '<h2>' . esc_html__('Status', 'wpcalc') . '</h2><table class="widefat striped" style="max-width:60rem"><tbody>';
        $this->status_row(__('Service running', 'wpcalc'), $running ? __('yes', 'wpcalc') : __('no', 'wpcalc'), $running);
        $this->status_row(__('Binary', 'wpcalc'), $binary, is_file($binary) && is_executable($binary));
        $this->status_row(__('proc_open available', 'wpcalc'), function_exists('proc_open') ? __('yes', 'wpcalc') : __('no', 'wpcalc'), function_exists('proc_open'));
        $this->status_row(__('curl available', 'wpcalc'), function_exists('curl_init') ? __('yes', 'wpcalc') : __('no', 'wpcalc'), function_exists('curl_init'));
        $this->status_row(__('Socket', 'wpcalc'), $this->socket_path(), file_exists($this->socket_path()));
        $this->status_row(__('Database', 'wpcalc'), $this->db_path(), file_exists($this->db_path()));
        $this->status_row(__('Log', 'wpcalc'), $this->log_path(), file_exists($this->log_path()));
        echo '</tbody></table>';

        $log = @file_get_contents($this->log_path());
        if (is_string($log) && $log !== '') {
            $tail = array_slice(preg_split('/\r?\n/', trim($log)), -12);
            echo '<h2>' . esc_html__('Recent log', 'wpcalc') . '</h2>';
            printf('<pre style="max-width:60rem;overflow:auto;background:#fff;border:1px solid #ccd0d4;padding:.75rem">%s</pre>',
                esc_html(implode("\n", $tail)));
        }

        echo '<h2>' . esc_html__('Configuration', 'wpcalc') . '</h2>';
        echo '<form method="post"><table class="form-table"><tbody>';
        printf(
            '<tr><th scope="row"><label for="%1$s">%2$s</label></th><td>'
            . '<input type="text" class="regular-text code" id="%1$s" name="%1$s" value="%3$s" placeholder="%4$s">'
            . '<p class="description">%5$s</p></td></tr>',
            esc_attr(self::OPT_BINARY),
            esc_html__('Binary path', 'wpcalc'),
            esc_attr((string) get_option(self::OPT_BINARY, '')),
            esc_attr(plugin_dir_path(__FILE__) . 'bin/wpcalc'),
            esc_html__('Leave empty to use the binary bundled with the plugin.', 'wpcalc')
        );
        printf(
            '<tr><th scope="row">%s</th><td><p class="description">%s</p></td></tr>',
            esc_html__('Shared secret', 'wpcalc'),
            esc_html__('Generated automatically and never displayed. It is what lets the service trust the identity this plugin asserts, so it is treated as a credential rather than a setting.', 'wpcalc')
        );
        echo '</tbody></table>';

        wp_nonce_field(self::NONCE_ACTION, self::NONCE_FIELD);
        submit_button(__('Save', 'wpcalc'), 'primary', 'wpcalc_save', false, ['form' => '']);
        echo '<input type="hidden" name="wpcalc_action" value="save">';
        echo '</form>';

        // Separate forms so each carries exactly one action.
        foreach ([
            'restart' => __('Restart service', 'wpcalc'),
            'rotate'  => __('Regenerate shared secret', 'wpcalc'),
        ] as $action => $label) {
            echo '<form method="post" style="display:inline-block;margin-right:.5rem">';
            wp_nonce_field(self::NONCE_ACTION, self::NONCE_FIELD);
            printf('<input type="hidden" name="wpcalc_action" value="%s">', esc_attr($action));
            printf('<button type="submit" class="button">%s</button>', esc_html($label));
            echo '</form>';
        }

        echo '</div>';
    }

    private function status_row(string $label, string $value, bool $ok): void
    {
        printf(
            '<tr><td style="width:14rem"><strong>%s</strong></td><td><code>%s</code> <span style="color:%s">%s</span></td></tr>',
            esc_html($label),
            esc_html($value),
            $ok ? '#1c6b3f' : '#a4243b',
            $ok ? '&#10003;' : '&#10007;'
        );
    }

    // ---------------------------------------------------------------- utils

    private function flatten_headers(array $headers): array
    {
        $out = [];
        foreach ($headers as $k => $v) {
            $out[] = $k . ': ' . $v;
        }
        return $out;
    }

    private function parse_headers(string $raw): array
    {
        $out = [];
        foreach (preg_split('/\r?\n/', $raw) as $line) {
            $parts = explode(':', $line, 2);
            if (count($parts) === 2) {
                $out[strtolower(trim($parts[0]))] = trim($parts[1]);
            }
        }
        return $out;
    }
}

WPCalc_Plugin::boot();
