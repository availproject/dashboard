module.exports = {
  apps: [
    {
      name: "dashboard",
      script: "./dashboard-server",
      args: "-config ./config.yaml",
      cwd: "/home/prabal/workspace/product-dashboard/dashboard",
      interpreter: "none",
      env_file: ".env",
      watch: false,
      autorestart: true,
      max_restarts: 10,
      restart_delay: 3000,
      log_date_format: "YYYY-MM-DD HH:mm:ss Z",
      error_file: "./logs/dashboard-error.log",
      out_file: "./logs/dashboard-out.log",
      merge_logs: true,
      kill_timeout: 30000,
    },
  ],
};
