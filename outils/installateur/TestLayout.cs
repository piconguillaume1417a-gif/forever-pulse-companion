using System;
using System.Drawing;
using System.Globalization;
using System.IO;
using System.Reflection;
using System.Threading;
using System.Windows.Forms;

// Loads the actual packaged UI without ever invoking its installation handler.
internal static class TestLayout
{
    private static void Require(bool condition, string message)
    {
        if (!condition) throw new InvalidOperationException(message);
    }

    private static void CheckText(Control parent)
    {
        foreach (Control control in parent.Controls)
        {
            if (!control.Visible) continue;
            if (control is Label || control is CheckBox || control is Button)
            {
                Size required = control.GetPreferredSize(new Size(control.Width, 0));
                Require(control.Height >= required.Height,
                    "Clipped text: " + control.Text + " (" + control.Height + " < " + required.Height + ")");
            }
            CheckText(control);
        }
    }

    private static bool Hangul(char c) { return c >= '\uAC00' && c <= '\uD7A3'; }

    // Break opportunities of Windows (DrawText WordBreak): a space, or either side of a
    // CJK character. Hangul counts as CJK there, so Windows may end a line between two
    // syllables of one Korean word; the setup must break Korean at spaces only.
    private static bool Breakable(string s, int i)
    {
        return s[i - 1] == ' ' || s[i] == ' ' || s[i - 1] >= '\u2E80' || s[i] >= '\u2E80';
    }

    private static void CheckHangulBreaks(Control parent)
    {
        foreach (Control control in parent.Controls)
        {
            CheckHangulBreaks(control);
            Label label = control as Label;
            if (label == null || !label.Visible || label.Text.Length == 0) continue;
            // A label may draw a text of its own (Korean pre-wrapped at spaces).
            PropertyInfo drawn = label.GetType().GetProperty("DrawnText");
            string text = drawn != null ? (string)drawn.GetValue(label, null) : label.Text;
            int width = label.ClientSize.Width - label.Padding.Horizontal;
            const TextFormatFlags single = TextFormatFlags.NoPrefix | TextFormatFlags.SingleLine;
            foreach (string paragraph in text.Split('\n'))
            {
                int start = 0;
                while (start < paragraph.Length)
                {
                    int end = start + 1;
                    while (end < paragraph.Length &&
                        TextRenderer.MeasureText(paragraph.Substring(start, end + 1 - start), label.Font, new Size(int.MaxValue, int.MaxValue), single).Width <= width)
                        end++;
                    if (end >= paragraph.Length) break;
                    int cut = end;
                    while (cut > start + 1 && !Breakable(paragraph, cut)) cut--;
                    if (!Breakable(paragraph, cut)) cut = end;
                    Require(!(Hangul(paragraph[cut - 1]) && Hangul(paragraph[cut])),
                        "Korean word broken between two syllables: " + paragraph.Substring(start, cut - start) + " | " + paragraph.Substring(cut));
                    start = cut;
                    while (start < paragraph.Length && paragraph[start] == ' ') start++;
                }
            }
        }
    }

    private static TextBox FindPath(Control parent)
    {
        foreach (Control control in parent.Controls)
        {
            TextBox path = control as TextBox;
            if (path != null) return path;
            path = FindPath(control);
            if (path != null) return path;
        }
        return null;
    }

    [STAThread]
    private static int Main(string[] args)
    {
        try
        {
            Thread.CurrentThread.CurrentUICulture = CultureInfo.GetCultureInfo(args[1]);
            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            Assembly setup = Assembly.LoadFrom(Path.GetFullPath(args[0]));
            Type windowType = setup.GetType("SetupWindow", true);
            Directory.CreateDirectory(args[2]);
            int cases = 0;
            foreach (int width in new int[] { 550, 650, 900 })
            foreach (int length in new int[] { 0, 260 })
            foreach (int fontSize in new int[] { 10, 14 })
            {
                using (Form window = (Form)Activator.CreateInstance(windowType, true))
                {
                    window.ShowInTaskbar = false;
                    window.Opacity = 0;
                    window.Show();
                    float scale;
                    using (Graphics graphics = window.CreateGraphics()) scale = graphics.DpiX / 96F;
                    window.ClientSize = new Size((int)(width * scale), (int)(480 * scale));
                    // The font family is the one Setup.cs chose for this language (never forced here);
                    // English name, since FontFamily.Name follows the UI culture (맑은 고딕).
                    Require(window.Font.FontFamily.GetName(1033) == (args[1] == "ko-KR" ? "Malgun Gothic" : "Segoe UI"),
                        "Unexpected setup font: " + window.Font.FontFamily.GetName(1033));
                    window.Font = new Font(window.Font.FontFamily, fontSize);
                    TextBox path = FindPath(window);
                    Require(path != null, "Installation path is missing");
                    if (length == 0)
                        // Short, realistic path without the name of the Windows account.
                        path.Text = @"C:\Users\Joueur\AppData\Local\Programs\ForeverPulseCompanion";
                    else
                    {
                        const string prefix = @"C:\Users\";
                        const string suffix = @"\AppData\Local\Programs\ForeverPulseCompanion";
                        path.Text = prefix + new string('W', length - prefix.Length - suffix.Length) + suffix;
                    }
                    path.Select(0, 0);
                    window.PerformLayout();
                    Application.DoEvents();
                    CheckText(window);
                    CheckHangulBreaks(window);
                    Require(path.ReadOnly && path.Multiline && path.WordWrap && path.TabStop,
                        "Installation path must wrap and remain selectable/read-only");
                    Point last = path.GetPositionFromCharIndex(path.TextLength - 1);
                    Require(last.Y + path.Font.Height <= path.ClientSize.Height, "Last path line is clipped");
                    Require(last.X >= 0 && last.X < path.ClientSize.Width, "Path is clipped horizontally");
                    TableLayoutPanel body = (TableLayoutPanel)path.Parent;
                    int bottom = body.Padding.Top;
                    foreach (Control control in body.Controls)
                    {
                        if (!control.Visible) continue;
                        Require(control.Top >= bottom, "Overlapping body rows: " + control.Text);
                        Require(control.Right <= body.ClientSize.Width - body.Padding.Right, "Body overflows horizontally");
                        bottom = control.Bottom;
                    }
                    foreach (string field in new string[] { "install", "close" })
                    {
                        Control button = (Control)windowType.GetField(field, BindingFlags.Instance | BindingFlags.NonPublic).GetValue(window);
                        Rectangle bounds = window.RectangleToClient(button.RectangleToScreen(button.ClientRectangle));
                        Require(window.ClientRectangle.Contains(bounds), "Action button is outside the window");
                    }
                    // Status text changes must resize its row rather than overlap the next one.
                    Label status = (Label)windowType.GetField("status", BindingFlags.Instance | BindingFlags.NonPublic).GetValue(window);
                    status.Text = args[1] == "fr-FR"
                        ? "Installation terminée. Cliquez sur Ouvrir pour lancer la version 0.10.0-rc.6, ou sur Fermer pour la lancer plus tard."
                        : args[1] == "ko-KR"
                        ? "설치가 완료되었습니다(버전 0.10.0-rc.6). 지금 실행하려면 “열기”, 나중에 실행하려면 “닫기”를 클릭하세요."
                        : "Setup complete. Click Open to start version 0.10.0-rc.6, or Close to start it later.";
                    window.PerformLayout();
                    Application.DoEvents();
                    CheckText(window);
                    CheckHangulBreaks(window);
                    Panel viewport = (Panel)body.Parent;
                    viewport.ScrollControlIntoView(status);
                    Rectangle statusBounds = viewport.RectangleToClient(status.RectangleToScreen(status.ClientRectangle));
                    Require(viewport.ClientRectangle.Contains(statusBounds), "Status cannot be reached by scrolling");
                    viewport.AutoScrollPosition = Point.Empty;
                    using (Bitmap image = new Bitmap(window.Width, window.Height))
                    {
                        window.DrawToBitmap(image, new Rectangle(Point.Empty, image.Size));
                        image.Save(Path.Combine(args[2], args[1] + "-" + width + "-path" + length + "-font" + fontSize + ".png"));
                    }
                    cases++;
                }
            }
            File.WriteAllText(Path.Combine(args[2], args[1] + ".txt"), "PASS: " + cases + " native layout cases; no installation executed.");
            return 0;
        }
        catch (Exception error)
        {
            File.WriteAllText(Path.Combine(args[2], args[1] + "-error.txt"), error.ToString());
            return 1;
        }
    }
}
