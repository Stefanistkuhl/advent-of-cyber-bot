from http.server import HTTPServer, BaseHTTPRequestHandler
import json

state = {"day": 5, "submissions": 0}


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/api/submissions/leaderboard":
            self.send_response(200)
            self.send_header("Content-type", "application/json")
            self.end_headers()
            data = [
                {
                    "username": "Player1",
                    "total_submissions": 15,
                    "challenge_points": 450.5,
                    "total_points": 1250.0,
                    "last_submission": "2025-12-08T20:00:00",
                },
                {
                    "username": "Player2",
                    "total_submissions": 12,
                    "challenge_points": 400.0,
                    "total_points": 1100.0,
                    "last_submission": "2025-12-08T19:30:00",
                },
            ]
            self.wfile.write(json.dumps(data).encode())
        elif self.path == "/api/submissions/windows":
            self.send_response(200)
            self.send_header("Content-type", "application/json")
            self.end_headers()
            data = [
                {
                    "day": state["day"],
                    "enabled": True,
                    "max_submissions": 50,
                    "current_submissions": state["submissions"],
                }
            ]
            self.wfile.write(json.dumps(data).encode())

    def do_POST(self):
        if self.path == "/test/day":
            state["day"] += 1
            state["submissions"] = 0
            self.send_response(200)
            self.send_header("Content-type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"day": state["day"]}).encode())

    def log_message(self, format, *args):
        pass


if __name__ == "__main__":
    print(state)
    HTTPServer(("localhost", 5000), Handler).serve_forever()
