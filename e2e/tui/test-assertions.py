"""Independent hand-written controls for terminal assertions."""
import json
import unittest

from run import assert_editor, assert_cursor, assert_no_replay, assert_resize_replay, assert_transcript, output_bytes


class AssertionControls(unittest.TestCase):
    def test_resize_generations(self):
        history = 'T0 startup\nG1[0000]\nG1[0001]'
        repair = '\x1b[2J\x1b[3J\x1b[H'
        assert_resize_replay(history + repair + history, 2)
        for invalid in [history, history + repair + 'G1[0000]',
                        history + repair + history + 'G1[0001]',
                        history + repair + 'G1[0001]G1[0000]',
                        history + repair + 'G1[0000]G1[0001]',
                        history + '\x1b[3J' + history,
                        history + repair + history + '\x1b[?1049h']:
            with self.subTest(raw=invalid), self.assertRaises(AssertionError):
                assert_resize_replay(invalid, 2)

    def test_exact_transcript(self):
        assert_transcript('G1[0000]\nG1[0001]', 2)
        for text in ['G1[0000]', 'G1[0000]G1[0000]G1[0001]', 'G1[0001]G1[0000]']:
            with self.subTest(text=text), self.assertRaises(AssertionError):
                assert_transcript(text, 2)

    def test_cursor(self):
        state = {'text': 'T0 startup\neditor> draft', 'cursor': {'x': 13, 'y': 1}}
        assert_cursor(state, 13, 'editor> draft')
        for cursor in [{'x': 12, 'y': 1}, {'x': 13, 'y': 0}]:
            with self.subTest(cursor=cursor), self.assertRaises(AssertionError):
                assert_cursor(dict(state, cursor=cursor), 13, 'editor> draft')
        with self.assertRaises(AssertionError):
            assert_cursor(dict(state, text='editor> draft\neditor> draft'), 13, 'editor> draft')

    def test_single_editor(self):
        assert_editor({'text': 'editor> abc界\n😀Z'}, 'editor> abc界')
        for text in ['editor> abc界\neditor> abc界\n😀Z', '界editor> ab\neditor> abc\n界😀Z']:
            with self.subTest(text=text), self.assertRaises(AssertionError):
                assert_editor({'text': text}, 'editor> abc界' if 'abc界' in text else 'editor> abc')

    def test_output_chunk_join(self):
        events = [{'version': 2}, [0.1, 'o', 'G1[00'], [0.2, 'i', 'G1[9999]'], [0.3, 'o', '00]\nG1[0001]']]
        cast = '\n'.join(json.dumps(event) for event in events)
        raw = output_bytes(cast)
        assert_no_replay(raw, 2)
        with self.assertRaises(AssertionError):
            assert_no_replay(raw + 'G1[0001]', 2)
        for erase in ['\x1b[2J', '\x1b[3J', '\x1b[?47h', '\x1b[?1047h', '\x1b[?1049h']:
            with self.subTest(erase=erase), self.assertRaises(AssertionError):
                assert_no_replay(raw + erase, 2)


if __name__ == '__main__':
    unittest.main()
